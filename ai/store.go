/*
Package ai provides the VectorStore contract, search options with metadata filtering,
the PgVectorStore production adapter, and the MemoryVectorStore testing fake.
*/
package ai

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/charledeon77/gostack-framework/framework/contract"
)

var identRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SearchOptions configures vector similarity queries, including metadata filtering and score cutoffs.
type SearchOptions struct {
	Limit    int            // Maximum records to return (default: 10)
	MinScore float64        // Minimum similarity score threshold (0.0 to 1.0)
	Filter   map[string]any // Metadata key-value filters (e.g. user_id = 42)
	Metric   DistanceMetric // Distance metric (default: CosineDistanceMetric <=>)
}

// VectorStore defines the pluggable contract for storing and querying vector embeddings.
type VectorStore interface {
	// Store inserts or updates a single Document.
	Store(ctx context.Context, doc Document) error

	// StoreBatch inserts or updates multiple Documents in a single operation.
	StoreBatch(ctx context.Context, docs []Document) error

	// Search executes similarity search using the target query vector and options.
	Search(ctx context.Context, query Vector, opts SearchOptions) (SearchResults, error)

	// Delete removes a document by its ID.
	Delete(ctx context.Context, id any) error
}

// ============================================================================
// MemoryVectorStore — Zero-Dependency In-Memory Store for Fast Unit Tests
// ============================================================================

// MemoryVectorStore provides a fast, thread-safe in-memory vector store.
// Designed for unit tests, local prototyping, and testing RAG pipelines without
// requiring a running PostgreSQL instance or Docker container.
type MemoryVectorStore struct {
	mu   sync.RWMutex
	docs map[any]Document
}

// NewMemoryVectorStore constructs an empty in-memory vector store.
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{
		docs: make(map[any]Document),
	}
}

// Store saves a document into memory.
func (m *MemoryVectorStore) Store(ctx context.Context, doc Document) error {
	if doc.ID == nil {
		return fmt.Errorf("ai: document ID cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = doc
	return nil
}

// StoreBatch saves a batch of documents into memory.
func (m *MemoryVectorStore) StoreBatch(ctx context.Context, docs []Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, doc := range docs {
		if doc.ID == nil {
			return fmt.Errorf("ai: document ID cannot be nil")
		}
		m.docs[doc.ID] = doc
	}
	return nil
}

// Search queries stored documents using in-memory vector distance and metadata filters.
func (m *MemoryVectorStore) Search(ctx context.Context, query Vector, opts SearchOptions) (SearchResults, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	var matched SearchResults

	for _, doc := range m.docs {
		// Apply metadata filters
		if len(opts.Filter) > 0 {
			matches := true
			for k, v := range opts.Filter {
				if docVal, exists := doc.Metadata[k]; !exists || docVal != v {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}

		if len(doc.Embedding) == 0 {
			continue
		}

		dist, err := CosineDistance(query, doc.Embedding)
		if err != nil {
			continue
		}
		score := 1.0 - dist

		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}

		matched = append(matched, SearchResult{
			ID:       doc.ID,
			Content:  doc.Content,
			Metadata: doc.Metadata,
			Distance: dist,
			Score:    score,
		})
	}

	// Sort ascending by distance (descending by score)
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Distance < matched[j].Distance
	})

	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, nil
}

// Delete removes a document from memory by its ID.
func (m *MemoryVectorStore) Delete(ctx context.Context, id any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, id)
	return nil
}

// ============================================================================
// PgVectorStore — Production PostgreSQL Adapter
// ============================================================================

// PgVectorStore implements the VectorStore contract on top of PostgreSQL and pgvector.
type PgVectorStore struct {
	db        contract.Database
	tableName string
	vectorCol string
}

// NewPgVectorStore constructs a PgVectorStore bound to a GoStack Database instance.
func NewPgVectorStore(db contract.Database, tableName, vectorCol string) *PgVectorStore {
	if vectorCol == "" {
		vectorCol = "embedding"
	}
	if !identRegex.MatchString(tableName) {
		panic(fmt.Sprintf("ai: invalid table name identifier %q", tableName))
	}
	if !identRegex.MatchString(vectorCol) {
		panic(fmt.Sprintf("ai: invalid vector column identifier %q", vectorCol))
	}
	return &PgVectorStore{
		db:        db,
		tableName: tableName,
		vectorCol: vectorCol,
	}
}

// Store inserts or updates a document with its vector in PostgreSQL.
func (s *PgVectorStore) Store(ctx context.Context, doc Document) error {
	query := fmt.Sprintf(
		"INSERT INTO %s (id, content, %s) VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET content = $2, %s = $3;",
		s.tableName, s.vectorCol, s.vectorCol,
	)
	return s.db.Exec(query, doc.ID, doc.Content, doc.Embedding.String())
}

// StoreBatch inserts multiple documents into PostgreSQL within a single transaction.
func (s *PgVectorStore) StoreBatch(ctx context.Context, docs []Document) error {
	tx, err := s.db.BeginTx()
	if err != nil {
		return fmt.Errorf("ai: failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := fmt.Sprintf(
		"INSERT INTO %s (id, content, %s) VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET content = $2, %s = $3;",
		s.tableName, s.vectorCol, s.vectorCol,
	)

	for _, doc := range docs {
		if err := tx.Exec(query, doc.ID, doc.Content, doc.Embedding.String()); err != nil {
			return fmt.Errorf("ai: batch insert failed on id %v: %w", doc.ID, err)
		}
	}

	return tx.Commit()
}

// Search queries PostgreSQL using pgvector similarity operators and metadata filters.
func (s *PgVectorStore) Search(ctx context.Context, query Vector, opts SearchOptions) (SearchResults, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	op := string(opts.Metric)
	if op == "" {
		op = "<=>"
	}

	whereClauses := []string{}
	args := []any{query.String()}
	argIndex := 2

	// Dynamic metadata filtering
	for k, v := range opts.Filter {
		if !identRegex.MatchString(k) {
			return nil, fmt.Errorf("ai: invalid metadata filter column %q", k)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", k, argIndex))
		args = append(args, v)
		argIndex++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	sqlQuery := fmt.Sprintf(
		"SELECT id, content, (%s %s $1) AS distance FROM %s %s ORDER BY %s %s $1 ASC LIMIT $%d;",
		s.vectorCol, op, s.tableName, whereSQL, s.vectorCol, op, argIndex,
	)
	args = append(args, limit)

	res, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("ai: vector search query failed: %w", err)
	}

	rows, ok := res.(*sql.Rows)
	if !ok {
		return nil, nil
	}
	defer rows.Close()

	var results SearchResults
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Content, &r.Distance); err != nil {
			return nil, fmt.Errorf("ai: row scan failed: %w", err)
		}
		r.Score = 1.0 - r.Distance
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai: rows iteration error: %w", err)
	}

	return results, nil
}

// Delete removes a document by ID.
func (s *PgVectorStore) Delete(ctx context.Context, id any) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1;", s.tableName)
	return s.db.Exec(query, id)
}
