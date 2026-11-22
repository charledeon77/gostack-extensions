# GoStack AI Extension (`gostack-extensions/ai`)

The official AI Data, Ingestion, Retrieval, Prompt Composition, and Model Context Protocol (MCP) tooling extension for the **GoStack** fullstack Go web framework.

---

## 🌟 The GoStack AI Philosophy

In alignment with GoStack's **Zero-Dependency Principle** and **One Binary / One Mental Model** philosophy:

1. **Framework Owns the Foundation:**
   * **Vector Data Layer (`VectorStore`)**: Pluggable storage contract with production PostgreSQL (`PgVectorStore`) and in-memory (`MemoryVectorStore`) adapters, supporting metadata filtering and similarity cutoffs.
   * **Ingestion & Preprocessing**: Recursive natural boundary text chunking (`RecursiveChunker`), markup sanitization (`SanitizeText`), and token estimation (`EstimateTokens`).
   * **Prompting & Context Windowing**: Reusable templates (`PromptTemplate`), multi-turn chat history with sliding-window truncation (`Conversation.Window(lastN)`), and structured output extraction (`ExtractJSON`).
   * **Tooling & Agent Interoperability**: Standalone tool catalog (`ToolRegistry`) with export to OpenAI schemas (`ToOpenAITools`) and full Model Context Protocol (`MCPServer`) server integration.

2. **Application Owns the AI Provider:**
   * Application developers choose and configure whichever LLM provider SDK fits their needs. GoStack core never takes on volatile, version-breaking third-party AI vendor dependencies.

3. **Streaming Built on Native Infrastructure (GowSocket):**
   * > **💡 Streaming Architecture Note:** For streaming real-time token responses (e.g. typing animations in chat UIs), **do not reinvent Server-Sent Events (SSE)**. Pipe your LLM SDK's token stream directly into GoStack's built-in **GowSocket** WebSocket hub (`framework/socket`). Tokens flow over persistent WebSocket channels straight into your reactive **Glide** UI components with zero latency and zero external messaging queues.

---

## 📦 Installation

```bash
go get github.com/charledeon77/gostack-extensions/ai
```

---

## 🚀 Getting Started

### Register the Service Provider

In your GoStack application entrypoint (`main.go`):

```go
import (
    "github.com/charledeon77/gostack-extensions/ai"
    "github.com/charledeon77/gostack-framework/framework/foundation"
)

func main() {
    app := foundation.NewApp()
    app.RegisterProvider(ai.NewServiceProvider())
    // ...
}
```

---

## 🧠 Complete Feature Tour

### 1. Document Chunking & Text Sanitization

Split raw text or HTML documents along natural language boundaries while preserving an overlapping window:

```go
cleanText := ai.SanitizeText(rawHTML)
chunker := ai.NewRecursiveChunker(1000, 100) // 1000 chars, 100 char overlap
chunks := chunker.Chunk(cleanText)
```

---

### 2. Embeddings (`Embedder` & Zero-Dep HTTP Embedders)

Turn text into vectors using mock embedders for testing or standard HTTP clients for OpenAI / Ollama:

```go
// For deterministic unit testing without external APIs:
mockEmbedder := ai.NewMockEmbedder(1536)
vector, _ := mockEmbedder.Embed(ctx, "Hello GoStack")

// For OpenAI standard endpoints (includes automated exponential backoff on HTTP 429/5xx):
openAI := ai.NewOpenAIEmbedder(os.Getenv("OPENAI_API_KEY"), "text-embedding-3-small").
    WithRetry(3, 200*time.Millisecond) // up to 3 retries on rate limits
vector, _ := openAI.Embed(ctx, "Hello GoStack")

// For local Ollama instances:
ollama := ai.NewOllamaEmbedder("http://localhost:11434", "nomic-embed-text")
vector, _ := ollama.Embed(ctx, "Hello GoStack")
```

---

### 3. Vector Storage & Metadata Filtering (`VectorStore`)

Store and retrieve vectors with strict tenant metadata isolation and similarity score thresholds:

```go
// Production PostgreSQL + pgvector adapter:
store := ai.NewPgVectorStore(db, "knowledge_base", "embedding")

// Or In-Memory store for fast unit testing (sub-millisecond tests):
// store := ai.NewMemoryVectorStore()

// Store document:
_ = store.Store(ctx, ai.Document{
    ID:        "doc-42",
    Content:   "Payment terms and invoice settlement guide.",
    Embedding: vector,
    Metadata:  map[string]any{"user_id": 101, "category": "billing"},
})

// Search with multi-tenant filtering & minimum score cutoff:
results, _ := store.Search(ctx, queryVector, ai.SearchOptions{
    Limit:    5,
    MinScore: 0.75, // discard matches with score < 75%
    Filter:   map[string]any{"user_id": 101}, // isolated to user 101
})
```

---

### 4. Conversation History & Sliding Window Context Manager

Prevent LLM context window overflows by maintaining the system prompt and keeping the most recent conversation turns:

```go
conv := ai.NewConversation().
    AddSystem("You are a technical support agent.").
    AddUser("How do I deploy?").
    AddAssistant("Copy the single binary to your server.").
    AddUser("What about database migrations?").
    AddAssistant("Traveller runs migrations automatically.")

// Keeps system prompt + last 2 turns:
windowedMessages := conv.Window(2)
```

---

### 5. Structured Output Extraction (`ExtractJSON`)

Extract and parse JSON responses even when LLMs wrap the output in markdown code fences:

```go
rawResponse := "```json\n{\"sentiment\": \"positive\", \"score\": 0.98}\n```"

type Analysis struct {
    Sentiment string  `json:"sentiment"`
    Score     float64 `json:"score"`
}

var result Analysis
if err := ai.ExtractJSON(rawResponse, &result); err == nil {
    // result.Sentiment == "positive"
}
```

---

### 6. Decoupled Tool Registry & MCP Server

Declare tools once, use them for OpenAI function calling or expose them to AI coding agents via MCP:

```go
tools := ai.NewToolRegistry()
tools.Register(ai.Tool{
    Name:        "get_weather",
    Description: "Fetches current weather for a location",
    InputSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{"type": "string"},
        },
        "required": []string{"city"},
    },
    Handler: func(ctx context.Context, args map[string]any) (any, error) {
        return map[string]any{"temp": 72}, nil
    },
})

// Export to OpenAI tools format:
openAIToolDefinitions := tools.ToOpenAITools()

// Or serve securely via Model Context Protocol JSON-RPC 2.0 with API Key protection:
mcp := ai.NewMCPServer("MyGoStackApp", "v1.0.0").
    WithToolRegistry(tools).
    WithAPIKey(os.Getenv("MCP_SERVER_SECRET")) // Enforces Authorization: Bearer or X-API-Key

router.Post("/mcp", mcp.ServeHTTP)
```

---

### 7. PostgreSQL Vector Indexing at Scale (HNSW vs. IVFFlat)

At production scale (>100,000 vectors), searching without an index triggers full table sequential scans. GoStack provides automated DDL generators for both PostgreSQL vector index strategies:

| Index Type | Build Time | Query Speed | Memory (RAM) Usage | When to Use |
|---|---|---|---|---|
| **HNSW** (`Hierarchical Navigable Small World`) | Moderate | **Ultra-Fast (sub-ms)** | High | **Default for Real-Time Apps.** Best search recall and query throughput. Build immediately upon table creation. |
| **IVFFlat** (`Inverted File Flat`) | Very Fast | Fast | **Low (Memory-lean)** | **Best for Massive Data on Constrained VPS.** Divide vectors into clusters (`lists = rows / 1000`). Must be built *after* data is loaded. |

```go
// 🚀 Shipped Production Default: HNSW with Cosine distance
// Provides sub-millisecond approximate nearest neighbor lookups out of the box:
_ = db.Exec(ai.CreateDefaultIndexSQL("idx_docs_embedding", "knowledge_base", "embedding"))

// Or customize tuning parameters:
_ = db.Exec(ai.CreateHNSWIndexSQL("idx_docs_hnsw", "knowledge_base", "embedding", ai.CosineDistanceMetric, 16, 64))

// Or create memory-lean IVFFlat index for constrained RAM:
_ = db.Exec(ai.CreateIVFFlatIndexSQL("idx_docs_ivfflat", "knowledge_base", "embedding", ai.CosineDistanceMetric, 100))
```

---

## 🔄 The Full End-to-End RAG Lifecycle

```go
// 1. Ingest & sanitize raw content:
cleanText := ai.SanitizeText(emailBody)
chunks := ai.NewRecursiveChunker(500, 50).Chunk(cleanText)

// 2. Embed and index:
embedder := ai.NewOpenAIEmbedder(apiKey, "text-embedding-3-small")
for i, chunk := range chunks {
    vec, _ := embedder.Embed(ctx, chunk)
    _ = store.Store(ctx, ai.Document{
        ID:        fmt.Sprintf("email-%d", i),
        Content:   chunk,
        Embedding: vec,
        Metadata:  map[string]any{"user_id": userID},
    })
}

// 3. User search with tenant isolation:
queryVec, _ := embedder.Embed(ctx, userQuery)
results, _ := store.Search(ctx, queryVec, ai.SearchOptions{
    Limit:    3,
    MinScore: 0.70,
    Filter:   map[string]any{"user_id": userID},
})

// 4. Compose prompt cleanly:
template := ai.MustPromptTemplate("support", `Context:
{{.Context}}

User Question: {{.Question}}
Answer based only on the context above.`)

prompt, _ := template.Execute(map[string]string{
    "Context":  results.AsContext(), // Seamless top-K formatting
    "Question": userQuery,
})

// 5. Call LLM & stream tokens via GowSocket:
go func() {
    for token := range myLLMClient.Stream(ctx, prompt) {
        socketHub.BroadcastToUser(userID, "chat.token", token)
    }
}()
```
