# GoStack AI-Mongo — MongoDB Atlas Vector Search Adapter

[![Go Reference](https://pkg.go.dev/badge/github.com/charledeon77/gostack-extensions/ai-mongo.svg)](https://pkg.go.dev/github.com/charledeon77/gostack-extensions/ai-mongo)

MongoDB Atlas Vector Search adapter for the [GoStack AI extension](../ai). Implements the `ai.VectorStore` contract so GoStack applications backed by MongoDB can participate in the full RAG pipeline — vector storage, similarity search, metadata filtering, and batch upserts — using the same interface as `PgVectorStore`.

---

## Requirements

| Requirement | Detail |
|---|---|
| Go | 1.22 or later |
| MongoDB | Atlas cluster (M10 or higher recommended for production Vector Search) |
| Atlas Vector Search index | Must be created on your collection before calling `Search` |

> [!NOTE]
> MongoDB's `$vectorSearch` operator is an **Atlas feature**. It is not available on self-hosted MongoDB Community Edition. If you are running PostgreSQL, use [`gostack-extensions/ai`](../ai) directly — `PgVectorStore` is included there.

---

## Installation

```bash
# Install the core AI extension first (shared types: Document, Vector, VectorStore, etc.)
go get github.com/charledeon77/gostack-extensions/ai@v1.0.0

# Install this adapter
go get github.com/charledeon77/gostack-extensions/ai-mongo@v1.0.0
```

---

## Quick Start

```go
package main

import (
    "context"
    "log"

    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"

    "github.com/charledeon77/gostack-extensions/ai"
    aimongo "github.com/charledeon77/gostack-extensions/ai-mongo"
)

func main() {
    // 1. Connect to MongoDB Atlas
    client, _ := mongo.Connect(context.Background(),
        options.Client().ApplyURI("mongodb+srv://user:pass@cluster.mongodb.net"))
    defer client.Disconnect(context.Background())

    coll := client.Database("myapp").Collection("knowledge_base")

    // 2. Create the vector store
    store := aimongo.NewMongoVectorStore(coll, aimongo.Options{
        EmbeddingField: "embedding",    // BSON field holding the vector array
        ContentField:   "content",      // BSON field holding the document text
        IndexName:      "vector_index", // Atlas Vector Search index name
    })

    // 3. Store a document
    store.Store(context.Background(), ai.Document{
        ID:        "doc-1",
        Content:   "GoStack is a fullstack Go web framework.",
        Embedding: ai.Vector{0.12, 0.84, /* ... 1536 dims */},
        Metadata:  map[string]any{"tenant": "acme"},
    })

    // 4. Search
    queryVec := ai.Vector{0.11, 0.83, /* ... */}
    results, err := store.Search(context.Background(), queryVec, ai.SearchOptions{
        Limit:    5,
        MinScore: 0.75,
        Filter:   map[string]any{"tenant": "acme"},
    })
    if err != nil {
        log.Fatal(err)
    }

    // 5. Inject into a prompt
    context := results.AsContext()
    _ = context // pass to ai.PromptTemplate
}
```

---

## Creating the Atlas Vector Search Index

Before calling `Search`, you must register a Vector Search index on your collection. Use `AtlasIndexJSON` to generate the correct payload:

```go
store := aimongo.NewMongoVectorStore(coll, aimongo.Options{})
fmt.Println(store.AtlasIndexJSON(1536)) // 1536 = OpenAI text-embedding-3-small dimensions
```

Output:
```json
{
  "name": "vector_index",
  "type": "vectorSearch",
  "fields": [
    {
      "type": "vector",
      "path": "embedding",
      "numDimensions": 1536,
      "similarity": "cosine"
    }
  ]
}
```

**Register via Atlas UI:**
> Database → Search → Create Search Index → JSON Editor → paste the output above.

**Register via Atlas CLI:**
```bash
store.AtlasIndexJSON(1536) > index.json
atlas deployments search indexes create --clusterName <cluster> --file index.json
```

### Dimension reference

| Embedding model | Dimensions |
|---|---|
| OpenAI `text-embedding-3-small` | 1536 |
| OpenAI `text-embedding-3-large` | 3072 |
| Nomic `nomic-embed-text` | 768 |
| Ollama `mxbai-embed-large` | 1024 |

---

## API Reference

### `NewMongoVectorStore(collection *mongo.Collection, opts Options) *MongoVectorStore`

Constructs a MongoVectorStore. All `Options` fields are optional — defaults are applied if empty.

| Option | Default | Description |
|---|---|---|
| `EmbeddingField` | `"embedding"` | BSON field storing the `[]float32` vector array |
| `ContentField` | `"content"` | BSON field storing the document text |
| `IndexName` | `"vector_index"` | Name of the Atlas Vector Search index |

### `Store(ctx, doc) error`

Upserts a single `ai.Document`. If a document with the same `_id` already exists, its fields are overwritten.

### `StoreBatch(ctx, docs) error`

Upserts multiple documents in a single `BulkWrite` call. Rejects the entire batch before any writes if any document has a nil ID.

### `Search(ctx, query, opts) (SearchResults, error)`

Executes an Atlas `$vectorSearch` aggregation. Metadata filters (`opts.Filter`) are applied as Atlas pre-filters at the index level — not as post-aggregation `$match` stages — for maximum efficiency.

### `Delete(ctx, id) error`

Removes a document by `_id`. Succeeds silently if no document with that ID exists.

### `AtlasIndexJSON(dimensions int) string`

Returns the JSON payload to register the Vector Search index in Atlas. `dimensions` must match your embedding model's output size.

---

## Implementing a Custom VectorStore for Other Databases

If you are using Cassandra, Neo4j, Qdrant, or another database, implement the `ai.VectorStore` interface from the core extension:

```go
type VectorStore interface {
    Store(ctx context.Context, doc Document) error
    StoreBatch(ctx context.Context, docs []Document) error
    Search(ctx context.Context, query Vector, opts SearchOptions) (SearchResults, error)
    Delete(ctx context.Context, id any) error
}
```

Your custom adapter can then be used anywhere a `VectorStore` is accepted.

---

## License

MIT — see [LICENSE](../../LICENSE) in the root of this repository.
