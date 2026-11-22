package ai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charledeon77/gostack-framework/framework/contract"
	"github.com/charledeon77/gostack-framework/framework/foundation"
	_ "modernc.org/sqlite"
)

// 1. Vector serialization and scanning tests
func TestVector_Value_Scan(t *testing.T) {
	v := NewVector([]float32{1.5, -2.25, 0.0, 3.125})

	if v.Dimensions() != 4 {
		t.Fatalf("expected 4 dimensions, got %d", v.Dimensions())
	}

	val, err := v.Value()
	if err != nil {
		t.Fatalf("Value() returned unexpected error: %v", err)
	}
	valStr, ok := val.(string)
	if !ok || valStr != "[1.5,-2.25,0,3.125]" {
		t.Errorf("unexpected vector string format: %v", val)
	}

	var scanned Vector
	if err := scanned.Scan("[1.5, -2.25, 0, 3.125]"); err != nil {
		t.Fatalf("Scan() failed: %v", err)
	}
	if scanned.Dimensions() != 4 || scanned[0] != 1.5 || scanned[1] != -2.25 {
		t.Errorf("scanned values incorrect: %v", scanned)
	}

	var scannedBytes Vector
	if err := scannedBytes.Scan([]byte("[0.1, 0.2]")); err != nil || scannedBytes.Dimensions() != 2 {
		t.Errorf("Scan([]byte) failed: %v", err)
	}

	var nilVec Vector
	if err := nilVec.Scan(nil); err != nil || nilVec != nil {
		t.Errorf("expected nil vector scan to produce nil, got %v", nilVec)
	}
}

// 2. Vector distance calculations
func TestVector_Distances(t *testing.T) {
	v1 := NewVector([]float32{1.0, 0.0, 0.0})
	v2 := NewVector([]float32{0.0, 1.0, 0.0})
	v3 := NewVector([]float32{1.0, 0.0, 0.0})

	sim, err := CosineSimilarity(v1, v2)
	if err != nil || math.Abs(sim-0.0) > 1e-6 {
		t.Errorf("expected orthogonal cosine similarity 0.0, got %f (err: %v)", sim, err)
	}

	dist, err := CosineDistance(v1, v2)
	if err != nil || math.Abs(dist-1.0) > 1e-6 {
		t.Errorf("expected orthogonal cosine distance 1.0, got %f (err: %v)", dist, err)
	}

	distSame, err := CosineDistance(v1, v3)
	if err != nil || math.Abs(distSame-0.0) > 1e-6 {
		t.Errorf("expected identical cosine distance 0.0, got %f (err: %v)", distSame, err)
	}

	l2, err := L2Distance(v1, v2)
	if err != nil || math.Abs(l2-math.Sqrt(2)) > 1e-6 {
		t.Errorf("expected L2 distance %f, got %f", math.Sqrt(2), l2)
	}

	ip, err := InnerProduct(v1, v3)
	if err != nil || math.Abs(ip-1.0) > 1e-6 {
		t.Errorf("expected inner product 1.0, got %f", ip)
	}
}

// 3. Document chunking, sanitization, and token estimation
func TestDocument_ChunkerAndSanitizer(t *testing.T) {
	rawHTML := `<div>
		<h1>GoStack Architecture</h1>
		<p>GoStack is a fullstack framework for Go.</p>
		<p>It eliminates Node.js from production builds.</p>
	</div>`

	clean := SanitizeText(rawHTML)
	if strings.Contains(clean, "<div>") || strings.Contains(clean, "<h1>") {
		t.Errorf("SanitizeText failed to strip HTML tags: %s", clean)
	}
	if !strings.Contains(clean, "GoStack Architecture") {
		t.Errorf("SanitizeText corrupted content: %s", clean)
	}

	tokens := EstimateTokens("Hello world! This is a test string.")
	if tokens <= 0 || tokens > 20 {
		t.Errorf("unexpected token estimate: %d", tokens)
	}

	chunker := NewRecursiveChunker(50, 10)
	longText := "First paragraph about Citadel.\n\nSecond paragraph about Tempose.\n\nThird paragraph about Vault and object streaming."
	chunks := chunker.Chunk(longText)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d: %v", len(chunks), chunks)
	}
	for i, c := range chunks {
		if len(c) > 60 { // allow slight separator boundary
			t.Errorf("chunk %d exceeded limit: len=%d (%q)", i, len(c), c)
		}
	}
}

// 4. MemoryVectorStore with Metadata Filtering & MinScore
func TestMemoryVectorStore_SearchAndFilter(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()

	doc1 := Document{
		ID:        "doc-1",
		Content:   "Invoice payment terms and billing information.",
		Embedding: NewVector([]float32{1.0, 0.0, 0.0}),
		Metadata:  map[string]any{"user_id": 42, "category": "billing"},
	}
	doc2 := Document{
		ID:        "doc-2",
		Content:   "Engineering team architecture notes.",
		Embedding: NewVector([]float32{0.0, 1.0, 0.0}),
		Metadata:  map[string]any{"user_id": 99, "category": "engineering"},
	}
	doc3 := Document{
		ID:        "doc-3",
		Content:   "Invoice overdue reminder notice.",
		Embedding: NewVector([]float32{0.9, 0.1, 0.0}),
		Metadata:  map[string]any{"user_id": 42, "category": "billing"},
	}

	_ = store.StoreBatch(ctx, []Document{doc1, doc2, doc3})

	// Search for billing documents specifically for user 42
	query := NewVector([]float32{1.0, 0.0, 0.0})
	results, err := store.Search(ctx, query, SearchOptions{
		Limit:    5,
		MinScore: 0.8,
		Filter:   map[string]any{"user_id": 42},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 matching results for user 42 with score >= 0.8, got %d", len(results))
	}
	if results[0].ID != "doc-1" {
		t.Errorf("expected doc-1 as top match, got %v", results[0].ID)
	}

	// Delete doc-1
	_ = store.Delete(ctx, "doc-1")
	afterDelete, _ := store.Search(ctx, query, SearchOptions{Limit: 5, Filter: map[string]any{"user_id": 42}})
	if len(afterDelete) != 1 {
		t.Errorf("expected 1 result after deletion, got %d", len(afterDelete))
	}
}

// 5. MockEmbedder tests
func TestMockEmbedder(t *testing.T) {
	embedder := NewMockEmbedder(8)
	ctx := context.Background()

	vec, err := embedder.Embed(ctx, "hello world")
	if err != nil || vec.Dimensions() != 8 {
		t.Fatalf("MockEmbedder failed: %v (dim: %d)", err, vec.Dimensions())
	}

	batch, err := embedder.EmbedBatch(ctx, []string{"one", "two", "three"})
	if err != nil || len(batch) != 3 {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
}

// 6. Conversation and sliding window context manager
func TestConversation_Window(t *testing.T) {
	conv := NewConversation().
		AddSystem("You are an assistant.").
		AddUser("Turn 1").
		AddAssistant("Reply 1").
		AddUser("Turn 2").
		AddAssistant("Reply 2").
		AddUser("Turn 3").
		AddAssistant("Reply 3")

	if len(conv.Messages()) != 7 {
		t.Fatalf("expected 7 messages total, got %d", len(conv.Messages()))
	}

	// Window to last 2 turns (+ system prompt)
	windowed := conv.Window(2)
	if len(windowed) != 3 {
		t.Fatalf("expected 3 windowed messages (system + last 2), got %d", len(windowed))
	}
	if windowed[0].Role != RoleSystem {
		t.Errorf("expected windowed message 0 to be System, got %s", windowed[0].Role)
	}
	if windowed[1].Content != "Turn 3" || windowed[2].Content != "Reply 3" {
		t.Errorf("windowed turns incorrect: %+v", windowed)
	}

	// Last message helper
	last := conv.Last()
	if last == nil || last.Content != "Reply 3" {
		t.Errorf("expected last message 'Reply 3', got %v", last)
	}
}

// 7. Structured Output Extractor
func TestExtractJSON(t *testing.T) {
	type AnalysisResult struct {
		Sentiment  string  `json:"sentiment"`
		Confidence float64 `json:"confidence"`
	}

	// Case A: Markdown fenced with ```json
	fenced := "Here is your analysis:\n```json\n{\n  \"sentiment\": \"positive\",\n  \"confidence\": 0.95\n}\n```\nHope this helps!"
	var resA AnalysisResult
	if err := ExtractJSON(fenced, &resA); err != nil {
		t.Fatalf("ExtractJSON fenced failed: %v", err)
	}
	if resA.Sentiment != "positive" || resA.Confidence != 0.95 {
		t.Errorf("unexpected parsed values: %+v", resA)
	}

	// Case B: Pure raw JSON
	raw := `{"sentiment": "neutral", "confidence": 0.50}`
	var resB AnalysisResult
	if err := ExtractJSON(raw, &resB); err != nil {
		t.Fatalf("ExtractJSON raw failed: %v", err)
	}
	if resB.Sentiment != "neutral" {
		t.Errorf("unexpected parsed sentiment: %s", resB.Sentiment)
	}

	// Case C: Invalid input
	var resC AnalysisResult
	if err := ExtractJSON("Sorry, I cannot process this request.", &resC); err == nil {
		t.Error("expected error for non-JSON input, got nil")
	}
}

// 8. ToolRegistry & Format Export
func TestToolRegistry(t *testing.T) {
	reg := NewToolRegistry()

	err := reg.Register(Tool{
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
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	tool, ok := reg.Get("get_weather")
	if !ok || tool.Name != "get_weather" {
		t.Errorf("Get failed")
	}

	openAITools := reg.ToOpenAITools()
	if len(openAITools) != 1 {
		t.Fatalf("expected 1 OpenAI tool, got %d", len(openAITools))
	}
	fnMap := openAITools[0]["function"].(map[string]any)
	if fnMap["name"] != "get_weather" {
		t.Errorf("unexpected OpenAI tool name: %v", fnMap["name"])
	}
}

// 9. MCP Server integration
func TestMCPServer_HandshakeAndTools(t *testing.T) {
	server := NewMCPServer("Test-Server", "v1.0.0")

	_ = server.RegisterTool(Tool{
		Name:        "calculate_tax",
		Description: "Calculates total tax for an amount",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			amt, _ := args["amount"].(float64)
			rate, _ := args["rate"].(float64)
			return map[string]any{"tax": amt * rate}, nil
		},
	})

	// Handshake
	resp := server.HandleMessage(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize failed: %+v", resp)
	}

	// Tool call
	params, _ := json.Marshal(map[string]any{
		"name":      "calculate_tax",
		"arguments": map[string]any{"amount": 100.0, "rate": 0.15},
	})
	callResp := server.HandleMessage(context.Background(), JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  params,
	})
	if callResp == nil || callResp.Error != nil {
		t.Fatalf("tools/call failed: %+v", callResp)
	}

	toolRes := callResp.Result.(ToolResult)
	if !strings.Contains(toolRes.Content[0].Text, `"tax": 15`) {
		t.Errorf("unexpected output: %s", toolRes.Content[0].Text)
	}
}

// 10. ServiceProvider integration
func TestServiceProvider_Integration(t *testing.T) {
	container := foundation.NewContainer()
	provider := NewServiceProvider()

	provider.Register(container)
	provider.Boot(container)

	if _, err := container.Resolve("ai.tools"); err != nil {
		t.Fatalf("failed to resolve ai.tools: %v", err)
	}
	if _, err := container.Resolve("ai.prompts"); err != nil {
		t.Fatalf("failed to resolve ai.prompts: %v", err)
	}
	if _, err := container.Resolve("ai.mcp"); err != nil {
		t.Fatalf("failed to resolve ai.mcp: %v", err)
	}
	if _, err := container.Resolve("ai.store.memory"); err != nil {
		t.Fatalf("failed to resolve ai.store.memory: %v", err)
	}
}

// 11. Complete End-to-End RAG Pipeline test
func TestFullRAGPipeline(t *testing.T) {
	ctx := context.Background()

	// 1. Raw text arrives (e.g. from an email or documentation)
	rawInput := `<div>
		<h2>GoStack Deployment Guide</h2>
		<p>GoStack applications compile into a single static binary.</p>
		<p>Deploy by copying the binary to any Linux or Windows VPS.</p>
	</div>`

	// 2. Sanitize and chunk
	cleanText := SanitizeText(rawInput)
	chunker := NewRecursiveChunker(100, 20)
	chunks := chunker.Chunk(cleanText)

	// 3. Generate embeddings with MockEmbedder
	embedder := NewMockEmbedder(4)
	store := NewMemoryVectorStore()

	for i, chunk := range chunks {
		vec, err := embedder.Embed(ctx, chunk)
		if err != nil {
			t.Fatalf("embedding failed: %v", err)
		}
		_ = store.Store(ctx, Document{
			ID:        i + 1,
			Content:   chunk,
			Embedding: vec,
			Metadata:  map[string]any{"source": "deploy_guide"},
		})
	}

	// 4. Query with filter
	queryText := "How to deploy GoStack?"
	queryVec, _ := embedder.Embed(ctx, queryText)
	searchResults, err := store.Search(ctx, queryVec, SearchOptions{
		Limit:  2,
		Filter: map[string]any{"source": "deploy_guide"},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	// 5. Compose prompt via SearchResults.AsContext()
	contextBlock := searchResults.AsContext()
	promptTmpl := MustPromptTemplate("rag_deploy", `Context:
{{.Context}}

Question: {{.Question}}
Return your answer in structured JSON:
{"summary": "..."}`)

	promptStr, err := promptTmpl.Execute(map[string]string{
		"Context":  contextBlock,
		"Question": queryText,
	})
	if err != nil {
		t.Fatalf("prompt execution failed: %v", err)
	}

	if !strings.Contains(promptStr, "GoStack applications compile") {
		t.Errorf("prompt missing context block:\n%s", promptStr)
	}

	// 6. Simulate LLM output and parse JSON
	simulatedLLMOutput := "```json\n{\"summary\": \"Deploy by copying the single binary to any VPS.\"}\n```"
	var parsed struct {
		Summary string `json:"summary"`
	}
	if err := ExtractJSON(simulatedLLMOutput, &parsed); err != nil {
		t.Fatalf("ExtractJSON failed on simulated LLM output: %v", err)
	}
	if !strings.Contains(parsed.Summary, "copying the single binary") {
		t.Errorf("unexpected parsed output: %+v", parsed)
	}
}

// 12. HTTP MCP Server test
func TestMCPServer_HTTP(t *testing.T) {
	server := NewMCPServer("HTTP-MCP", "v1.0.0")

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
}

// 13. MCP Server API Key Authentication
func TestMCPServer_Authentication(t *testing.T) {
	server := NewMCPServer("Auth-MCP", "v1.0.0").WithAPIKey("secret-key-1234")

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	// Case A: Missing token -> 401
	reqMissing := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	wMissing := httptest.NewRecorder()
	server.ServeHTTP(wMissing, reqMissing)
	if wMissing.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized on missing token, got %d", wMissing.Code)
	}

	// Case B: Invalid token -> 401
	reqBad := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	reqBad.Header.Set("Authorization", "Bearer wrong-key")
	wBad := httptest.NewRecorder()
	server.ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized on wrong token, got %d", wBad.Code)
	}

	// Case C: Valid Authorization Bearer header -> 200
	reqBearer := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	reqBearer.Header.Set("Authorization", "Bearer secret-key-1234")
	wBearer := httptest.NewRecorder()
	server.ServeHTTP(wBearer, reqBearer)
	if wBearer.Code != http.StatusOK {
		t.Errorf("expected 200 OK with valid Bearer token, got %d", wBearer.Code)
	}

	// Case D: Valid X-API-Key header -> 200
	reqHeader := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(reqBody))
	reqHeader.Header.Set("X-API-Key", "secret-key-1234")
	wHeader := httptest.NewRecorder()
	server.ServeHTTP(wHeader, reqHeader)
	if wHeader.Code != http.StatusOK {
		t.Errorf("expected 200 OK with valid X-API-Key, got %d", wHeader.Code)
	}
}

// 14. Heavy Concurrency & Race Contention on MemoryVectorStore
func TestMemoryVectorStore_ConcurrentContention(t *testing.T) {
	store := NewMemoryVectorStore()
	ctx := context.Background()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			docID := fmt.Sprintf("concurrent-doc-%d", id)
			vec := NewVector([]float32{float32(id) * 0.01, 0.5, 0.5})

			// Write
			_ = store.Store(ctx, Document{
				ID:        docID,
				Content:   fmt.Sprintf("Concurrent content payload %d", id),
				Embedding: vec,
				Metadata:  map[string]any{"tenant": id % 3},
			})

			// Immediate concurrent search with filter
			_, _ = store.Search(ctx, vec, SearchOptions{
				Limit:  5,
				Filter: map[string]any{"tenant": id % 3},
			})
		}(i)
	}

	wg.Wait()
}

// 15. Minified document with zero whitespace
func TestRecursiveChunker_MinifiedNoWhitespace(t *testing.T) {
	chunker := NewRecursiveChunker(50, 10)

	// 500 characters of continuous alphanumeric data with zero spaces
	continuousText := strings.Repeat("ABCDEFGHIJ", 50)

	chunks := chunker.Chunk(continuousText)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks from continuous text, got none")
	}

	for i, c := range chunks {
		if len(c) > 60 {
			t.Errorf("chunk %d exceeded max size: len=%d", i, len(c))
		}
	}
}

// 16. HTTPEmbedder automatic backoff retry on HTTP 429
func TestHTTPEmbedder_RetryOn429(t *testing.T) {
	var attempts int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att < 3 {
			// First two attempts return 429 Too Many Requests
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		// Third attempt succeeds
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`))
	}))
	defer ts.Close()

	embedder := &HTTPEmbedder{
		client:         ts.Client(),
		baseURL:        ts.URL,
		model:          "test-model",
		maxRetries:     3,
		initialBackoff: 10 * time.Millisecond,
	}

	vec, err := embedder.Embed(context.Background(), "test retry")
	if err != nil {
		t.Fatalf("Embed should succeed after retries, but failed: %v", err)
	}

	if vec.Dimensions() != 3 {
		t.Errorf("expected 3 dimensions, got %d", vec.Dimensions())
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

// 17. Search query returning zero rows
func TestMemoryVectorStore_ZeroResults(t *testing.T) {
	store := NewMemoryVectorStore()
	ctx := context.Background()

	_ = store.Store(ctx, Document{
		ID:        "doc-a",
		Content:   "Alpha document",
		Embedding: NewVector([]float32{1.0, 0.0}),
		Metadata:  map[string]any{"org_id": "org_1"},
	})

	// Search for an org that does not exist
	results, err := store.Search(ctx, NewVector([]float32{1.0, 0.0}), SearchOptions{
		Limit:  10,
		Filter: map[string]any{"org_id": "non_existent_org"},
	})
	if err != nil {
		t.Fatalf("Search failed unexpectedly on zero matches: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	if results.AsContext() != "" {
		t.Errorf("expected empty string context for zero results, got %q", results.AsContext())
	}
}

// 18. DDL and Shipped Default Index Strategy
func TestVector_DDL_AndDefault(t *testing.T) {
	defaultSQL := CreateDefaultIndexSQL("idx_default", "documents", "embedding")
	if !strings.Contains(defaultSQL, "USING hnsw (embedding vector_cosine_ops)") {
		t.Errorf("expected default index to be HNSW with vector_cosine_ops, got: %s", defaultSQL)
	}
	if !strings.Contains(defaultSQL, "WITH (m = 16, ef_construction = 64)") {
		t.Errorf("expected default HNSW params m=16, ef=64, got: %s", defaultSQL)
	}

	extSQL := EnablePgVectorSQL()
	if extSQL != "CREATE EXTENSION IF NOT EXISTS vector;" {
		t.Errorf("unexpected extension SQL: %s", extSQL)
	}

	colSQL := AddVectorColumnSQL("documents", "embedding", 1536)
	if colSQL != "ALTER TABLE documents ADD COLUMN embedding vector(1536);" {
		t.Errorf("unexpected column SQL: %s", colSQL)
	}
}

// panicReader implements io.Reader and panics if Read is ever called.
type panicReader struct {
	readCalled *int32
}

func (r *panicReader) Read(p []byte) (n int, err error) {
	atomic.AddInt32(r.readCalled, 1)
	return 0, fmt.Errorf("unexpected Read() call on unauthenticated request")
}

// 19. MCP Server verifies authentication BEFORE body is ever read (Anti-DoS Defense)
func TestMCPServer_AuthCheckedBeforeBodyRead(t *testing.T) {
	server := NewMCPServer("Secure-MCP", "v1.0.0").WithAPIKey("secret-pass-token")

	var readCount int32
	badReq := httptest.NewRequest(http.MethodPost, "/mcp", &panicReader{readCalled: &readCount})
	// No Authorization or X-API-Key header provided
	w := httptest.NewRecorder()

	server.ServeHTTP(w, badReq)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}

	if atomic.LoadInt32(&readCount) != 0 {
		t.Errorf("CRITICAL SECURITY FLAW: Body was read %d times before authentication check!", readCount)
	}
}

// mockSQLAdapter wraps a real database/sql connection conforming to contract.Database.
type mockSQLAdapter struct {
	db *sql.DB
}

func (m *mockSQLAdapter) Connect() error                     { return nil }
func (m *mockSQLAdapter) Driver() string                    { return "sqlite" }
func (m *mockSQLAdapter) Close() error                      { return m.db.Close() }
func (m *mockSQLAdapter) BeginTx() (contract.Tx, error)     { return nil, nil }
func (m *mockSQLAdapter) Exec(sql string, args ...any) error {
	_, err := m.db.Exec(sql, args...)
	return err
}
func (m *mockSQLAdapter) Query(query string, args ...any) (any, error) {
	// Execute zero-row query returning real *sql.Rows with matching column schema (id, content, distance)
	return m.db.Query("SELECT id, content, 0.0 AS distance FROM knowledge_base WHERE 1=0")
}

// 20. Live Database returning Zero Rows to PgVectorStore.Search (Cursor handling)
func TestPgVectorStore_ZeroRows(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}
	defer rawDB.Close()

	// Create table and insert 0 rows
	_, err = rawDB.Exec("CREATE TABLE knowledge_base (id TEXT, content TEXT, embedding TEXT);")
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	adapter := &mockSQLAdapter{db: rawDB}
	store := NewPgVectorStore(adapter, "knowledge_base", "embedding")

	// Search returns empty result set from live SQL query
	results, err := store.Search(context.Background(), NewVector([]float32{1.0, 0.0}), SearchOptions{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("PgVectorStore.Search failed on zero live rows: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results from empty table, got %d", len(results))
	}
	if results.AsContext() != "" {
		t.Errorf("expected empty string context, got %q", results.AsContext())
	}
}
