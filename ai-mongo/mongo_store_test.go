package aimongo_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/charledeon77/gostack-extensions/ai"
	aimongo "github.com/charledeon77/gostack-extensions/ai-mongo"
)

// ============================================================================
// mockCursor — in-process cursor driven by a []bson.M slice.
// Satisfies the internal mongoCursor interface without a live MongoDB connection.
// ============================================================================

type mockCursor struct {
	docs    []bson.M
	idx     int
	lastErr error
}

func (m *mockCursor) Next(_ context.Context) bool {
	m.idx++
	return m.idx <= len(m.docs)
}

func (m *mockCursor) Decode(val interface{}) error {
	if m.idx < 1 || m.idx > len(m.docs) {
		return errors.New("cursor: out of range")
	}
	// Round-trip through BSON bytes so the target is populated correctly.
	b, err := bson.Marshal(m.docs[m.idx-1])
	if err != nil {
		return err
	}
	return bson.Unmarshal(b, val)
}

func (m *mockCursor) Close(_ context.Context) error { return nil }
func (m *mockCursor) Err() error                    { return m.lastErr }

// ============================================================================
// mockCollection — satisfies the internal mongoCollection interface.
// ============================================================================

type mockCollection struct {
	updateOneFunc func(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	bulkWriteFunc func(ctx context.Context, models []mongo.WriteModel, opts ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error)
	aggregateFunc func(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (aimongo.MongoCursor, error)
	deleteOneFunc func(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)
}

func (m *mockCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return m.updateOneFunc(ctx, filter, update, opts...)
}
func (m *mockCollection) BulkWrite(ctx context.Context, models []mongo.WriteModel, opts ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error) {
	return m.bulkWriteFunc(ctx, models, opts...)
}
func (m *mockCollection) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
	return m.aggregateFunc(ctx, pipeline, opts...)
}
func (m *mockCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return m.deleteOneFunc(ctx, filter, opts...)
}

// newTestStore builds a MongoVectorStore backed by the given mock collection.
func newTestStore(mc *mockCollection, opts aimongo.Options) *aimongo.MongoVectorStore {
	return aimongo.NewMongoVectorStoreFromCollection(mc, opts)
}

// ============================================================================
// Store tests
// ============================================================================

func TestMongoVectorStore_Store_Success(t *testing.T) {
	mc := &mockCollection{
		updateOneFunc: func(_ context.Context, _ interface{}, _ interface{}, _ ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
			return &mongo.UpdateResult{UpsertedCount: 1}, nil
		},
	}
	err := newTestStore(mc, aimongo.Options{}).Store(context.Background(), ai.Document{
		ID:        "doc-1",
		Content:   "hello world",
		Embedding: ai.Vector{0.1, 0.2, 0.3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMongoVectorStore_Store_NilID(t *testing.T) {
	mc := &mockCollection{}
	err := newTestStore(mc, aimongo.Options{}).Store(context.Background(), ai.Document{Content: "no id"})
	if err == nil {
		t.Fatal("expected error for nil document ID, got nil")
	}
}

func TestMongoVectorStore_Store_DriverError(t *testing.T) {
	mc := &mockCollection{
		updateOneFunc: func(_ context.Context, _ interface{}, _ interface{}, _ ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
			return nil, errors.New("network timeout")
		},
	}
	err := newTestStore(mc, aimongo.Options{}).Store(context.Background(), ai.Document{ID: "x", Content: "fail"})
	if err == nil {
		t.Fatal("expected wrapped driver error, got nil")
	}
}

func TestMongoVectorStore_Store_CustomFields(t *testing.T) {
	called := false
	mc := &mockCollection{
		updateOneFunc: func(_ context.Context, _ interface{}, update interface{}, _ ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
			called = true
			d := update.(bson.D)
			setDoc := d[0].Value.(bson.D)
			foundBody, foundVec := false, false
			for _, e := range setDoc {
				if e.Key == "body" {
					foundBody = true
				}
				if e.Key == "vec" {
					foundVec = true
				}
			}
			if !foundBody || !foundVec {
				return nil, errors.New("custom field names not used")
			}
			return &mongo.UpdateResult{UpsertedCount: 1}, nil
		},
	}
	err := newTestStore(mc, aimongo.Options{ContentField: "body", EmbeddingField: "vec"}).
		Store(context.Background(), ai.Document{ID: "x", Content: "test", Embedding: ai.Vector{0.5}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("UpdateOne was not called")
	}
}

// ============================================================================
// StoreBatch tests
// ============================================================================

func TestMongoVectorStore_StoreBatch_Success(t *testing.T) {
	mc := &mockCollection{
		bulkWriteFunc: func(_ context.Context, models []mongo.WriteModel, _ ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error) {
			if len(models) != 2 {
				return nil, fmt.Errorf("expected 2 models, got %d", len(models))
			}
			return &mongo.BulkWriteResult{UpsertedCount: 2}, nil
		},
	}
	err := newTestStore(mc, aimongo.Options{}).StoreBatch(context.Background(), []ai.Document{
		{ID: "a", Content: "first", Embedding: ai.Vector{0.1}},
		{ID: "b", Content: "second", Embedding: ai.Vector{0.2}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMongoVectorStore_StoreBatch_Empty(t *testing.T) {
	mc := &mockCollection{
		bulkWriteFunc: func(_ context.Context, _ []mongo.WriteModel, _ ...*options.BulkWriteOptions) (*mongo.BulkWriteResult, error) {
			return nil, errors.New("BulkWrite must not be called for empty batch")
		},
	}
	store := newTestStore(mc, aimongo.Options{})
	if err := store.StoreBatch(context.Background(), nil); err != nil {
		t.Fatalf("nil batch should return nil, got: %v", err)
	}
	if err := store.StoreBatch(context.Background(), []ai.Document{}); err != nil {
		t.Fatalf("empty slice should return nil, got: %v", err)
	}
}

func TestMongoVectorStore_StoreBatch_NilIDInBatch(t *testing.T) {
	mc := &mockCollection{}
	err := newTestStore(mc, aimongo.Options{}).StoreBatch(context.Background(), []ai.Document{
		{ID: "ok", Content: "fine"},
		{Content: "missing id"},
	})
	if err == nil {
		t.Fatal("expected error for nil document ID in batch")
	}
}

// ============================================================================
// Delete tests
// ============================================================================

func TestMongoVectorStore_Delete_Success(t *testing.T) {
	mc := &mockCollection{
		deleteOneFunc: func(_ context.Context, filter interface{}, _ ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
			d := filter.(bson.D)
			if d[0].Key != "_id" || d[0].Value != "doc-1" {
				return nil, errors.New("unexpected filter")
			}
			return &mongo.DeleteResult{DeletedCount: 1}, nil
		},
	}
	if err := newTestStore(mc, aimongo.Options{}).Delete(context.Background(), "doc-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMongoVectorStore_Delete_DriverError(t *testing.T) {
	mc := &mockCollection{
		deleteOneFunc: func(_ context.Context, _ interface{}, _ ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
			return nil, errors.New("write concern error")
		},
	}
	if err := newTestStore(mc, aimongo.Options{}).Delete(context.Background(), "x"); err == nil {
		t.Fatal("expected error from driver, got nil")
	}
}

// ============================================================================
// Search tests
// ============================================================================

func TestMongoVectorStore_Search_ReturnsResults(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return &mockCursor{docs: []bson.M{
				{"_id": "doc-1", "content": "first result", "score": 0.95},
				{"_id": "doc-2", "content": "second result", "score": 0.80},
			}}, nil
		},
	}
	results, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1, 0.2}, ai.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "first result" {
		t.Errorf("expected 'first result', got %q", results[0].Content)
	}
	if results[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %v", results[0].Score)
	}
	wantDist := 1.0 - 0.95
	if math.Abs(results[0].Distance-wantDist) > 1e-6 {
		t.Errorf("expected distance close to %v, got %v", wantDist, results[0].Distance)
	}
}

func TestMongoVectorStore_Search_Empty(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return &mockCursor{docs: []bson.M{}}, nil
		},
	}
	results, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1}, ai.SearchOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestMongoVectorStore_Search_MinScoreFiltering(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return &mockCursor{docs: []bson.M{
				{"_id": "a", "content": "high", "score": 0.90},
				{"_id": "b", "content": "low", "score": 0.50},
			}}, nil
		},
	}
	results, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1}, ai.SearchOptions{Limit: 10, MinScore: 0.75})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after MinScore filter, got %d", len(results))
	}
	if results[0].Content != "high" {
		t.Errorf("expected 'high', got %q", results[0].Content)
	}
}

func TestMongoVectorStore_Search_DefaultLimit(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return &mockCursor{docs: []bson.M{}}, nil
		},
	}
	_, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1}, ai.SearchOptions{Limit: 0})
	if err != nil {
		t.Fatalf("unexpected error with zero limit: %v", err)
	}
}

func TestMongoVectorStore_Search_MetadataDecoded(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return &mockCursor{docs: []bson.M{
				{
					"_id":      "doc-meta",
					"content":  "with metadata",
					"score":    0.88,
					"metadata": bson.M{"tenant": "acme", "user_id": int32(42)},
				},
			}}, nil
		},
	}
	results, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1}, ai.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Metadata["tenant"] != "acme" {
		t.Errorf("expected tenant=acme, got %v", results[0].Metadata["tenant"])
	}
}

func TestMongoVectorStore_Search_AggregateError(t *testing.T) {
	mc := &mockCollection{
		aggregateFunc: func(_ context.Context, _ interface{}, _ ...*options.AggregateOptions) (aimongo.MongoCursor, error) {
			return nil, errors.New("atlas connection refused")
		},
	}
	_, err := newTestStore(mc, aimongo.Options{}).Search(context.Background(), ai.Vector{0.1}, ai.SearchOptions{})
	if err == nil {
		t.Fatal("expected error from aggregate, got nil")
	}
}

// ============================================================================
// AtlasIndexJSON test
// ============================================================================

func TestMongoVectorStore_AtlasIndexJSON(t *testing.T) {
	mc := &mockCollection{}
	store := newTestStore(mc, aimongo.Options{EmbeddingField: "embedding", IndexName: "my_vector_idx"})
	j := store.AtlasIndexJSON(1536)

	for _, want := range []string{`"my_vector_idx"`, `"vectorSearch"`, `"embedding"`, `1536`, `"cosine"`} {
		found := false
		for i := 0; i <= len(j)-len(want); i++ {
			if j[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AtlasIndexJSON: missing %q\nGot:\n%s", want, j)
		}
	}
}
