/*
Package ai provides standalone tool definition and a catalog registry.
*/
package ai

import (
	"context"
	"fmt"
	"sync"
)

// ToolHandler represents the execution callback for an application tool.
type ToolHandler func(ctx context.Context, arguments map[string]any) (any, error)

// Tool defines an invokable application function exposed to AI agents and models.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

// ToolRegistry maintains a thread-safe catalog of application tools.
// It decouples tool definition from any specific transport or protocol (MCP, OpenAI, CLI).
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry constructs a fresh tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) error {
	if tool.Name == "" {
		return fmt.Errorf("ai: tool name cannot be empty")
	}
	if tool.Handler == nil {
		return fmt.Errorf("ai: tool %q must have an execution handler", tool.Name)
	}
	if tool.InputSchema == nil {
		tool.InputSchema = map[string]any{
			"type": "object",
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
	return nil
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ToOpenAITools formats the registered tools into standard OpenAI function calling JSON schema.
func (r *ToolRegistry) ToOpenAITools() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []map[string]any
	for _, t := range r.tools {
		list = append(list, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	return list
}
