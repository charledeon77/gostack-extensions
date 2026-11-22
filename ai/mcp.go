/*
Package ai implements the Model Context Protocol (MCP) server for GoStack.

MCP (Model Context Protocol) is an open standard that allows AI coding assistants,
chatbots, and autonomous agents (such as Claude Code, Cursor, Windsurf, Antigravity)
to securely discover and invoke application tools, read resources, and load
structured prompts directly from the running GoStack application.
*/
package ai

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// MCPProtocolVersion defines the supported protocol version.
const MCPProtocolVersion = "2024-11-05"

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError defines an error structure compliant with JSON-RPC 2.0.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ToolContent represents a content item returned by an MCP tool invocation.
type ToolContent struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// ToolResult represents the output of a tools/call execution.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// MCPServer manages tools, prompts, and JSON-RPC dispatch for Model Context Protocol.
type MCPServer struct {
	mu             sync.RWMutex
	name           string
	version        string
	apiKey         string
	toolRegistry   *ToolRegistry
	promptRegistry *PromptRegistry
}

// NewMCPServer constructs a new Model Context Protocol server.
func NewMCPServer(name, version string) *MCPServer {
	if name == "" {
		name = "GoStack-MCP-Server"
	}
	if version == "" {
		version = "v1.0.0"
	}
	return &MCPServer{
		name:           name,
		version:        version,
		toolRegistry:   NewToolRegistry(),
		promptRegistry: NewPromptRegistry(),
	}
}

// WithAPIKey sets a required API key for authenticating incoming MCP HTTP requests.
func (s *MCPServer) WithAPIKey(key string) *MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKey = key
	return s
}

// WithToolRegistry sets a custom ToolRegistry for the MCP server.
func (s *MCPServer) WithToolRegistry(tr *ToolRegistry) *MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolRegistry = tr
	return s
}

// WithPromptRegistry sets a custom PromptRegistry for the MCP server.
func (s *MCPServer) WithPromptRegistry(pr *PromptRegistry) *MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptRegistry = pr
	return s
}

// RegisterTool registers an executable tool with the underlying ToolRegistry.
func (s *MCPServer) RegisterTool(tool Tool) error {
	return s.toolRegistry.Register(tool)
}

// RegisterPrompt registers a PromptTemplate with the underlying PromptRegistry.
func (s *MCPServer) RegisterPrompt(pt *PromptTemplate) error {
	return s.promptRegistry.Register(pt)
}

// Tools returns the underlying ToolRegistry.
func (s *MCPServer) Tools() *ToolRegistry {
	return s.toolRegistry
}

// Prompts returns the underlying PromptRegistry.
func (s *MCPServer) Prompts() *PromptRegistry {
	return s.promptRegistry
}

// HandleMessage processes a single JSON-RPC 2.0 request and returns the response.
func (s *MCPServer) HandleMessage(ctx context.Context, req JSONRPCRequest) *JSONRPCResponse {
	// Notifications (no ID) do not return responses
	isNotification := req.ID == nil

	switch req.Method {
	case "initialize":
		result := map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
				"prompts": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		}
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "notifications/initialized":
		return nil

	case "ping":
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	case "tools/list":
		tools := s.toolRegistry.List()
		toolList := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			toolList = append(toolList, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}

		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": toolList,
			},
		}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return &JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &JSONRPCError{
						Code:    CodeInvalidParams,
						Message: fmt.Sprintf("Invalid params: %v", err),
					},
				}
			}
		}

		tool, exists := s.toolRegistry.Get(params.Name)
		if !exists {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeMethodNotFound,
					Message: fmt.Sprintf("Tool %q not found", params.Name),
				},
			}
		}

		out, err := tool.Handler(ctx, params.Arguments)
		if err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: ToolResult{
					IsError: true,
					Content: []ToolContent{
						{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
					},
				},
			}
		}

		var textOutput string
		switch v := out.(type) {
		case string:
			textOutput = v
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			textOutput = string(b)
		}

		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolResult{
				Content: []ToolContent{
					{Type: "text", Text: textOutput},
				},
			},
		}

	case "prompts/list":
		pts := s.promptRegistry.List()
		promptList := make([]map[string]any, 0, len(pts))
		for _, pt := range pts {
			promptList = append(promptList, map[string]any{
				"name":        pt.Name(),
				"description": pt.Description(),
			})
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"prompts": promptList,
			},
		}

	case "prompts/get":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return &JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &JSONRPCError{
						Code:    CodeInvalidParams,
						Message: fmt.Sprintf("Invalid params: %v", err),
					},
				}
			}
		}

		pt, ok := s.promptRegistry.Get(params.Name)
		if !ok {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeMethodNotFound,
					Message: fmt.Sprintf("Prompt %q not found", params.Name),
				},
			}
		}

		rendered, err := pt.Execute(params.Arguments)
		if err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInternalError,
					Message: fmt.Sprintf("Failed to render prompt: %v", err),
				},
			}
		}

		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"description": pt.Description(),
				"messages": []map[string]any{
					{
						"role": "user",
						"content": map[string]any{
							"type": "text",
							"text": rendered,
						},
					},
				},
			},
		}

	default:
		if isNotification {
			return nil
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeMethodNotFound,
				Message: fmt.Sprintf("Method %q not found", req.Method),
			},
		}
	}
}

// ServeHTTP implements http.Handler, accepting JSON-RPC 2.0 payloads via HTTP POST.
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed: MCP expects POST requests", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate if an API key is configured
	s.mu.RLock()
	requiredKey := s.apiKey
	s.mu.RUnlock()

	if requiredKey != "" {
		token := r.Header.Get("X-API-Key")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(requiredKey)) != 1 {
			http.Error(w, "Unauthorized: invalid or missing API key", http.StatusUnauthorized)
			return
		}
	}

	// Limit body to 10MB to guard against memory exhaustion DoS
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &JSONRPCError{Code: CodeParseError, Message: "Failed to read request body"},
		})
		return
	}

	trimmed := string(body)
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\n' || trimmed[0] == '\r') {
		trimmed = trimmed[1:]
	}

	if len(trimmed) > 0 && trimmed[0] == '[' {
		var requests []JSONRPCRequest
		if err := json.Unmarshal(body, &requests); err != nil {
			s.writeJSON(w, http.StatusOK, JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: CodeParseError, Message: "Invalid JSON batch"},
			})
			return
		}

		var responses []*JSONRPCResponse
		for _, req := range requests {
			resp := s.HandleMessage(r.Context(), req)
			if resp != nil {
				responses = append(responses, resp)
			}
		}
		s.writeJSON(w, http.StatusOK, responses)
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeJSON(w, http.StatusOK, JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &JSONRPCError{Code: CodeParseError, Message: "Parse error: invalid JSON"},
		})
		return
	}

	resp := s.HandleMessage(r.Context(), req)
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *MCPServer) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
