/*
Package aimongo provides a MongoDB Atlas Vector Search adapter for the GoStack AI extension.

It implements the ai.VectorStore contract on top of MongoDB Atlas, allowing GoStack
applications backed by MongoDB to participate in the full RAG pipeline — vector storage,
similarity search, metadata filtering, and batch upserts — using the same interface as
the PostgreSQL pgvector adapter.

Requirements:
  - A MongoDB Atlas cluster (M10 or higher for production Vector Search)
  - A Vector Search index on the target collection (see MongoVectorStore.AtlasIndexJSON)

The developer brings their own *mongo.Collection; this package wraps Atlas's $vectorSearch
aggregation pipeline behind GoStack's VectorStore contract.
*/
package aimongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/charledeon77/gostack-extensions/ai"
)

// Options configures a MongoVectorStore instance.
type Options struct {
	// EmbeddingField is the BSON field name that stores the vector embedding array.
	// Defaults to "embedding".
	EmbeddingField string

	// ContentField is the BSON field name that stores the document text content.
	// Defaults to "content".
	ContentField string

	// IndexName is the name of the Atlas Vector Search index on the collection.
	// This must match the index name registered in the Atlas UI or Atlas CLI.
	// Defaults to "vector_index".
	IndexName string
}

// ============================================================================
// Internal interfaces — enable full unit testing without a live Atlas connection.
// ============================================================================

// MongoCursor is the internal cursor interface. *mongo.Cursor satisfies it in production;
// a lightweight mockCursor satisfies it in tests.
type MongoCursor interface {
	Next(ctx context.Context) bool
	Decode(val interface{}) error
	Close(ctx context.Context) error
	Err() error
}

// mongoCollection is the internal collection interface. *concreteCollection satisfies it
// in production (wrapping *mongo.Collection); *mockCollection satisfies it in tests.
type mongoCollection interface {
	UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	BulkWrite(ctx context.Context, models []mongo.WriteModel, opts ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error)
	Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (MongoCursor, error)
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)
}

// concreteCollection wraps *mongo.Collection to bridge its Aggregate return type
// (*mongo.Cursor) to the internal MongoCursor interface.
type concreteCollection struct {
	coll *mongo.Collection
}

func (c *concreteCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.coll.UpdateOne(ctx, filter, update, opts...)
}

func (c *concreteCollection) BulkWrite(ctx context.Context, models []mongo.WriteModel, opts ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error) {
	return c.coll.BulkWrite(ctx, models, opts...)
}

func (c *concreteCollection) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (MongoCursor, error) {
	return c.coll.Aggregate(ctx, pipeline, opts...)
}

func (c *concreteCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return c.coll.DeleteOne(ctx, filter, opts...)
}

// ============================================================================
// MongoVectorStore
// ============================================================================

// MongoVectorStore implements ai.VectorStore on top of MongoDB Atlas Vector Search.
// It wraps Atlas's $vectorSearch aggregation stage behind GoStack's standard VectorStore
// interface, providing the same Store/StoreBatch/Search/Delete API as PgVectorStore.
//
// Construction:
//
//	coll := mongoClient.Database("myapp").Collection("knowledge_base")
//	store := aimongo.NewMongoVectorStore(coll, aimongo.Options{})
type MongoVectorStore struct {
	coll           mongoCollection
	embeddingField string
	contentField   string
	indexName      string
}

// NewMongoVectorStore constructs a MongoVectorStore bound to the given MongoDB collection.
// The collection must exist and have an Atlas Vector Search index (see AtlasIndexJSON).
// All Options fields are optional — defaults are applied if empty.
func NewMongoVectorStore(collection *mongo.Collection, opts Options) *MongoVectorStore {
	if collection == nil {
		panic("ai-mongo: collection cannot be nil")
	}
	return newMongoVectorStore(&concreteCollection{coll: collection}, opts)
}

// NewMongoVectorStoreFromCollection constructs a MongoVectorStore from any value that
// satisfies the internal mongoCollection interface. This is the test-injection entry point.
func NewMongoVectorStoreFromCollection(coll mongoCollection, opts Options) *MongoVectorStore {
	return newMongoVectorStore(coll, opts)
}

func newMongoVectorStore(coll mongoCollection, opts Options) *MongoVectorStore {
	if opts.EmbeddingField == "" {
		opts.EmbeddingField = "embedding"
	}
	if opts.ContentField == "" {
		opts.ContentField = "content"
	}
	if opts.IndexName == "" {
		opts.IndexName = "vector_index"
	}
	return &MongoVectorStore{
		coll:           coll,
		embeddingField: opts.EmbeddingField,
		contentField:   opts.ContentField,
		indexName:      opts.IndexName,
	}
}

// Store upserts a single Document into the MongoDB collection.
// If a document with the same ID already exists, its content, embedding, and metadata are replaced.
func (s *MongoVectorStore) Store(ctx context.Context, doc ai.Document) error {
	if doc.ID == nil {
		return fmt.Errorf("ai-mongo: document ID cannot be nil")
	}
	filter := bson.D{{Key: "_id", Value: doc.ID}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: s.contentField, Value: doc.Content},
		{Key: s.embeddingField, Value: []float32(doc.Embedding)},
		{Key: "metadata", Value: doc.Metadata},
	}}}
	_, err := s.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("ai-mongo: store failed: %w", err)
	}
	return nil
}

// StoreBatch upserts multiple Documents in a single BulkWrite operation.
// If any document has a nil ID, the entire batch is rejected before any writes are sent.
func (s *MongoVectorStore) StoreBatch(ctx context.Context, docs []ai.Document) error {
	if len(docs) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(docs))
	for _, doc := range docs {
		if doc.ID == nil {
			return fmt.Errorf("ai-mongo: document ID cannot be nil")
		}
		filter := bson.D{{Key: "_id", Value: doc.ID}}
		update := bson.D{{Key: "$set", Value: bson.D{
			{Key: s.contentField, Value: doc.Content},
			{Key: s.embeddingField, Value: []float32(doc.Embedding)},
			{Key: "metadata", Value: doc.Metadata},
		}}}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true))
	}
	_, err := s.coll.BulkWrite(ctx, models)
	if err != nil {
		return fmt.Errorf("ai-mongo: batch store failed: %w", err)
	}
	return nil
}

// Search executes a MongoDB Atlas $vectorSearch aggregation and returns ranked results.
//
// It uses the cosine similarity score returned by Atlas as the result Score,
// and sets Distance = 1.0 - Score to maintain convention with PgVectorStore.
// Results are already sorted by Atlas in descending score order.
//
// Requires a Vector Search index registered on the collection. Use AtlasIndexJSON
// to generate the index definition JSON.
func (s *MongoVectorStore) Search(ctx context.Context, query ai.Vector, opts ai.SearchOptions) (ai.SearchResults, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	vectorSearchDoc := bson.D{
		{Key: "index", Value: s.indexName},
		{Key: "path", Value: s.embeddingField},
		{Key: "queryVector", Value: []float32(query)},
		{Key: "numCandidates", Value: limit * 10},
		{Key: "limit", Value: limit},
	}

	// Apply metadata pre-filter inside $vectorSearch for index-level filtering.
	// More efficient than a post-aggregation $match.
	if len(opts.Filter) > 0 {
		filterDoc := bson.D{}
		for k, v := range opts.Filter {
			filterDoc = append(filterDoc, bson.E{Key: k, Value: v})
		}
		vectorSearchDoc = append(vectorSearchDoc, bson.E{Key: "filter", Value: filterDoc})
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$vectorSearch", Value: vectorSearchDoc}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "score", Value: bson.D{{Key: "$meta", Value: "vectorSearchScore"}}},
		}}},
	}

	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("ai-mongo: vector search failed: %w", err)
	}
	defer cursor.Close(ctx)

	var results ai.SearchResults
	for cursor.Next(ctx) {
		var raw bson.M
		if err := cursor.Decode(&raw); err != nil {
			return nil, fmt.Errorf("ai-mongo: document decode failed: %w", err)
		}

		score, _ := raw["score"].(float64)
		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}

		content, _ := raw[s.contentField].(string)

		var metadata map[string]any
		if m, ok := raw["metadata"].(bson.M); ok {
			metadata = make(map[string]any, len(m))
			for k, v := range m {
				metadata[k] = v
			}
		}

		results = append(results, ai.SearchResult{
			ID:       raw["_id"],
			Content:  content,
			Metadata: metadata,
			Score:    score,
			Distance: 1.0 - score,
		})
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("ai-mongo: cursor iteration error: %w", err)
	}

	return results, nil
}

// Delete removes a document from the collection by its ID.
// If no document with that ID exists, the operation succeeds silently.
func (s *MongoVectorStore) Delete(ctx context.Context, id any) error {
	_, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return fmt.Errorf("ai-mongo: delete failed: %w", err)
	}
	return nil
}

// AtlasIndexJSON returns the JSON payload required to create the Vector Search index
// in the MongoDB Atlas UI or via the Atlas CLI / Admin API.
//
// dimensions must match the output size of your embedding model:
//   - OpenAI text-embedding-3-small  → 1536
//   - OpenAI text-embedding-3-large  → 3072
//   - Nomic nomic-embed-text         → 768
//   - Ollama mxbai-embed-large       → 1024
//
// Register the index in Atlas UI: Database → Search → Create Search Index → JSON Editor.
// Or via Atlas CLI: atlas deployments search indexes create --clusterName <name> --file index.json
func (s *MongoVectorStore) AtlasIndexJSON(dimensions int) string {
	return fmt.Sprintf(`{
  "name": %q,
  "type": "vectorSearch",
  "fields": [
    {
      "type": "vector",
      "path": %q,
      "numDimensions": %d,
      "similarity": "cosine"
    }
  ]
}`, s.indexName, s.embeddingField, dimensions)
}

// compile-time check: MongoVectorStore must satisfy ai.VectorStore.
var _ ai.VectorStore = (*MongoVectorStore)(nil)
