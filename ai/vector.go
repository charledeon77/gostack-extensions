/*
Package ai provides the official AI Data, Retrieval, Prompting, and Agent Tooling
extension for the GoStack fullstack framework.

Design Philosophy:
GoStack follows the Zero-Dependency Principle. Rather than importing heavyweight,
fast-deprecating third-party AI provider SDKs into core, this extension provides
the three foundational pillars that live cleanly at the framework layer:
 1. pgvector Data & Retrieval Layer: Storing high-dimensional embeddings in PostgreSQL
    and executing similarity search queries with Crafter.
 2. PromptTemplate & Composition Layer: Composing structured, reusable prompts using
    Go's standard library with zero runtime reflection glue.
 3. Model Context Protocol (MCP) Server: A standards-compliant JSON-RPC 2.0 server
    exposing GoStack application tools, routes, and schemas to AI coding agents.

Application developers bring their own Go LLM SDK (OpenAI, Anthropic, Gemini, Ollama)
for text generation, and utilize GoStack's native GowSocket subsystem for real-time
token streaming to browser clients.
*/
package ai

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// DistanceMetric represents the similarity distance operator used by pgvector.
type DistanceMetric string

const (
	// CosineDistanceMetric computes cosine distance (1 - cosine_similarity). Operator: <=>
	CosineDistanceMetric DistanceMetric = "<=>"
	// L2DistanceMetric computes Euclidean distance. Operator: <->
	L2DistanceMetric DistanceMetric = "<->"
	// InnerProductMetric computes negative dot product. Operator: <#>
	InnerProductMetric DistanceMetric = "<#>"
)

// Vector represents a dense high-dimensional floating point embedding vector.
// It implements database/sql.Scanner and database/sql/driver.Valuer for seamless
// integration with PostgreSQL and pgvector.
type Vector []float32

// NewVector constructs a new Vector from a slice of float32s.
func NewVector(vals []float32) Vector {
	v := make(Vector, len(vals))
	copy(v, vals)
	return v
}

// Dimensions returns the dimensionality (length) of the vector.
func (v Vector) Dimensions() int {
	return len(v)
}

// Slice returns the underlying []float32 slice.
func (v Vector) Slice() []float32 {
	return []float32(v)
}

// String formats the vector into the pgvector literal string format: "[0.1,0.2,...]"
func (v Vector) String() string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// Value implements the database/sql/driver.Valuer interface.
// It serializes the Vector into the string format expected by pgvector.
func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	return v.String(), nil
}

// Scan implements the database/sql.Scanner interface.
// It decodes a pgvector literal string (e.g. "[0.1,0.2,0.3]") or byte slice into a Vector.
func (v *Vector) Scan(src any) error {
	if src == nil {
		*v = nil
		return nil
	}

	var raw string
	switch s := src.(type) {
	case string:
		raw = s
	case []byte:
		raw = string(s)
	case []float32:
		*v = make(Vector, len(s))
		copy(*v, s)
		return nil
	default:
		return fmt.Errorf("ai: cannot scan type %T into Vector", src)
	}

	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return fmt.Errorf("ai: invalid vector format %q, missing brackets", raw)
	}

	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		*v = Vector{}
		return nil
	}

	parts := strings.Split(inner, ",")
	res := make(Vector, len(parts))
	for i, p := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("ai: failed to parse vector coordinate %q at index %d: %w", p, i, err)
		}
		res[i] = float32(val)
	}

	*v = res
	return nil
}

// CosineDistance calculates the cosine distance between two vectors:
// 1 - (A . B) / (||A|| * ||B||)
func CosineDistance(a, b Vector) (float64, error) {
	sim, err := CosineSimilarity(a, b)
	if err != nil {
		return 0, err
	}
	return 1.0 - sim, nil
}

// CosineSimilarity calculates the cosine similarity between two vectors:
// (A . B) / (||A|| * ||B||)
func CosineSimilarity(a, b Vector) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("ai: vector dimension mismatch (%d vs %d)", len(a), len(b))
	}
	if len(a) == 0 {
		return 0, fmt.Errorf("ai: cannot compute similarity of empty vectors")
	}

	var dot, normA, normB float64
	for i := 0; i < len(a); i++ {
		va := float64(a[i])
		vb := float64(b[i])
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0, fmt.Errorf("ai: cannot compute cosine similarity with zero-norm vector")
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), nil
}

// L2Distance calculates the Euclidean (L2) distance between two vectors:
// sqrt(sum((a_i - b_i)^2))
func L2Distance(a, b Vector) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("ai: vector dimension mismatch (%d vs %d)", len(a), len(b))
	}
	var sum float64
	for i := 0; i < len(a); i++ {
		diff := float64(a[i]) - float64(b[i])
		sum += diff * diff
	}
	return math.Sqrt(sum), nil
}

// InnerProduct calculates the dot product between two vectors:
// sum(a_i * b_i)
func InnerProduct(a, b Vector) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("ai: vector dimension mismatch (%d vs %d)", len(a), len(b))
	}
	var sum float64
	for i := 0; i < len(a); i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum, nil
}

// SearchResult represents a single retrieved entity with its similarity score.
type SearchResult struct {
	ID       any            `json:"id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Distance float64        `json:"distance"`
	Score    float64        `json:"score"` // Typically 1 - distance for cosine
}

// SearchResults is an ordered slice of retrieved SearchResult entries.
type SearchResults []SearchResult

// AsContext concatenates the contents of all search results into a clean,
// double-newline separated text block ready for injection into a PromptTemplate.
// This directly bridges vector search results with prompt composition.
func (sr SearchResults) AsContext() string {
	if len(sr) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, r := range sr {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		if r.Content != "" {
			sb.WriteString(r.Content)
		}
	}
	return sb.String()
}

// EnablePgVectorSQL returns the SQL query needed to enable the pgvector extension.
func EnablePgVectorSQL() string {
	return "CREATE EXTENSION IF NOT EXISTS vector;"
}

// AddVectorColumnSQL generates the DDL SQL to add a vector column to a PostgreSQL table.
func AddVectorColumnSQL(table, column string, dimensions int) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s vector(%d);", table, column, dimensions)
}

// CreateDefaultIndexSQL creates the framework's recommended default index (HNSW with Cosine distance).
// HNSW provides sub-millisecond approximate nearest neighbor retrieval out of the box and is
// the default recommended index for production applications.
func CreateDefaultIndexSQL(indexName, table, column string) string {
	return CreateHNSWIndexSQL(indexName, table, column, CosineDistanceMetric, 16, 64)
}

// CreateHNSWIndexSQL generates DDL SQL for an HNSW (Hierarchical Navigable Small World) index.
// Useful for high-scale, ultra-fast approximate nearest neighbor lookups.
func CreateHNSWIndexSQL(indexName, table, column string, metric DistanceMetric, m, efConstruction int) string {
	ops := "vector_cosine_ops"
	switch metric {
	case L2DistanceMetric:
		ops = "vector_l2_ops"
	case InnerProductMetric:
		ops = "vector_ip_ops"
	}
	return fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (%s %s) WITH (m = %d, ef_construction = %d);",
		indexName, table, column, ops, m, efConstruction,
	)
}

// CreateIVFFlatIndexSQL generates DDL SQL for an IVFFlat index.
func CreateIVFFlatIndexSQL(indexName, table, column string, metric DistanceMetric, lists int) string {
	ops := "vector_cosine_ops"
	switch metric {
	case L2DistanceMetric:
		ops = "vector_l2_ops"
	case InnerProductMetric:
		ops = "vector_ip_ops"
	}
	return fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING ivfflat (%s %s) WITH (lists = %d);",
		indexName, table, column, ops, lists,
	)
}

// VectorQueryBuilder builds parameterized SQL queries for similarity search on PostgreSQL.
type VectorQueryBuilder struct {
	table        string
	vectorColumn string
	selectCols   []string
	metric       DistanceMetric
	limit        int
}

// NewVectorQuery creates a query builder for vector similarity search.
func NewVectorQuery(table, vectorColumn string) *VectorQueryBuilder {
	return &VectorQueryBuilder{
		table:        table,
		vectorColumn: vectorColumn,
		selectCols:   []string{"id", "content"},
		metric:       CosineDistanceMetric,
		limit:        10,
	}
}

// Select specifies the non-vector columns to retrieve.
func (b *VectorQueryBuilder) Select(cols ...string) *VectorQueryBuilder {
	if len(cols) > 0 {
		b.selectCols = cols
	}
	return b
}

// Metric sets the distance metric (<=>, <->, or <#>). Default is Cosine (<=>).
func (b *VectorQueryBuilder) Metric(m DistanceMetric) *VectorQueryBuilder {
	b.metric = m
	return b
}

// Limit sets the maximum number of nearest neighbors to return.
func (b *VectorQueryBuilder) Limit(limit int) *VectorQueryBuilder {
	if limit > 0 {
		b.limit = limit
	}
	return b
}

// Build compiles the SQL query and parameter list.
// The target vector is passed as parameter $1, and the limit as $2.
func (b *VectorQueryBuilder) Build(target Vector) (string, []any) {
	colList := strings.Join(b.selectCols, ", ")
	op := string(b.metric)
	if op == "" {
		op = "<=>"
	}

	query := fmt.Sprintf(
		"SELECT %s, (%s %s $1) AS distance FROM %s ORDER BY %s %s $1 ASC LIMIT $2",
		colList, b.vectorColumn, op, b.table, b.vectorColumn, op,
	)

	return query, []any{target.String(), b.limit}
}
