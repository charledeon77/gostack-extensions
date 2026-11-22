/*
Package ai provides embedding contracts, testing fakes, and standard HTTP embedders.
*/
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder defines the contract for turning text into high-dimensional vector representations.
type Embedder interface {
	// Embed transforms a single text string into a vector.
	Embed(ctx context.Context, text string) (Vector, error)

	// EmbedBatch transforms multiple text strings into vectors in a single request.
	EmbedBatch(ctx context.Context, texts []string) ([]Vector, error)
}

// Reranker defines the contract for re-scoring retrieved candidates for semantic precision.
type Reranker interface {
	Rerank(ctx context.Context, query string, docs SearchResults, topN int) (SearchResults, error)
}

// ============================================================================
// MockEmbedder — Deterministic Embedding Generator for Unit Testing
// ============================================================================

// MockEmbedder produces deterministic vector embeddings for tests based on string length and hashing.
type MockEmbedder struct {
	Dimensions int
}

// NewMockEmbedder creates a mock embedder with fixed dimensionality.
func NewMockEmbedder(dimensions int) *MockEmbedder {
	if dimensions <= 0 {
		dimensions = 4
	}
	return &MockEmbedder{Dimensions: dimensions}
}

// Embed generates a deterministic vector for testing without network requests.
func (m *MockEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	vec := make(Vector, m.Dimensions)
	val := float32(len(text)) / 100.0
	for i := 0; i < m.Dimensions; i++ {
		vec[i] = val + float32(i)*0.1
	}
	return vec, nil
}

// EmbedBatch generates deterministic vectors for a slice of texts.
func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]Vector, error) {
	out := make([]Vector, len(texts))
	for i, t := range texts {
		v, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// ============================================================================
// HTTPEmbedder — Zero-Dependency Standard HTTP Embedder (OpenAI / Ollama)
// ============================================================================

// HTTPEmbedder communicates with standard OpenAI-compatible (/v1/embeddings)
// or Ollama (/api/embeddings) endpoints using pure standard library net/http.
type HTTPEmbedder struct {
	client         *http.Client
	baseURL        string
	apiKey         string
	model          string
	isOllama       bool
	maxRetries     int
	initialBackoff time.Duration
}

// NewOpenAIEmbedder creates an embedder pointing to OpenAI's standard API.
func NewOpenAIEmbedder(apiKey, model string) *HTTPEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &HTTPEmbedder{
		client:         &http.Client{Timeout: 30 * time.Second},
		baseURL:        "https://api.openai.com/v1/embeddings",
		apiKey:         apiKey,
		model:          model,
		maxRetries:     3,
		initialBackoff: 200 * time.Millisecond,
	}
}

// NewOllamaEmbedder creates an embedder pointing to a local or remote Ollama instance.
func NewOllamaEmbedder(baseURL, model string) *HTTPEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &HTTPEmbedder{
		client:         &http.Client{Timeout: 30 * time.Second},
		baseURL:        baseURL + "/api/embeddings",
		model:          model,
		isOllama:       true,
		maxRetries:     3,
		initialBackoff: 200 * time.Millisecond,
	}
}

// WithRetry configures the maximum retry attempts and initial backoff for rate limits and server errors.
func (h *HTTPEmbedder) WithRetry(maxRetries int, initialBackoff time.Duration) *HTTPEmbedder {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if initialBackoff <= 0 {
		initialBackoff = 200 * time.Millisecond
	}
	h.maxRetries = maxRetries
	h.initialBackoff = initialBackoff
	return h
}

// Embed calls the embedding endpoint for a single string.
func (h *HTTPEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	vecs, err := h.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("ai: embedder returned zero vectors")
	}
	return vecs[0], nil
}

// EmbedBatch sends texts to the embedding API and parses response vectors.
func (h *HTTPEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]Vector, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	if h.isOllama {
		// Ollama processes embeddings per text item
		var results []Vector
		for _, t := range texts {
			vec, err := h.embedSingleOllama(ctx, t)
			if err != nil {
				return nil, err
			}
			results = append(results, vec)
		}
		return results, nil
	}

	// Standard OpenAI format
	payload, _ := json.Marshal(map[string]any{
		"model": h.model,
		"input": texts,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}

	body, err := h.doWithRetry(ctx, req, payload)
	if err != nil {
		return nil, fmt.Errorf("ai: openai embedding request failed: %w", err)
	}

	var openAIResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, fmt.Errorf("ai: failed to decode openai embedding response: %w", err)
	}

	results := make([]Vector, len(openAIResp.Data))
	for _, item := range openAIResp.Data {
		if item.Index < len(results) {
			results[item.Index] = NewVector(item.Embedding)
		}
	}

	return results, nil
}

func (h *HTTPEmbedder) embedSingleOllama(ctx context.Context, text string) (Vector, error) {
	payload, _ := json.Marshal(map[string]string{
		"model":  h.model,
		"prompt": text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := h.doWithRetry(ctx, req, payload)
	if err != nil {
		return nil, fmt.Errorf("ai: ollama embedding request failed: %w", err)
	}

	var oResp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &oResp); err != nil {
		return nil, fmt.Errorf("ai: failed to decode ollama embedding: %w", err)
	}
	return NewVector(oResp.Embedding), nil
}

func (h *HTTPEmbedder) doWithRetry(ctx context.Context, req *http.Request, payload []byte) ([]byte, error) {
	maxRetries := h.maxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	backoff := h.initialBackoff
	if backoff <= 0 {
		backoff = 200 * time.Millisecond
	}

	for attempt := 0; ; attempt++ {
		req.Body = io.NopCloser(bytes.NewReader(payload))

		resp, err := h.client.Do(req)
		if err != nil {
			if attempt >= maxRetries || ctx.Err() != nil {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ai: http error (%d): %s", resp.StatusCode, string(body))
		}

		return body, nil
	}
}
