/*
Package ai provides the GoStack IoC ServiceProvider integration.
*/
package ai

import (
	"github.com/charledeon77/gostack-framework/framework/foundation"
)

// ServiceProvider integrates the AI extension into the GoStack application lifecycle.
type ServiceProvider struct {
	mcpServer      *MCPServer
	toolRegistry   *ToolRegistry
	promptRegistry *PromptRegistry
	memoryStore    *MemoryVectorStore
}

// NewServiceProvider constructs a fresh ServiceProvider instance.
func NewServiceProvider() *ServiceProvider {
	tools := NewToolRegistry()
	prompts := NewPromptRegistry()
	mcp := NewMCPServer("GoStack-AI-Extension", "v1.0.0").
		WithToolRegistry(tools).
		WithPromptRegistry(prompts)

	return &ServiceProvider{
		mcpServer:      mcp,
		toolRegistry:   tools,
		promptRegistry: prompts,
		memoryStore:    NewMemoryVectorStore(),
	}
}

// Register binds AI services into GoStack's IoC service container.
func (sp *ServiceProvider) Register(c *foundation.Container) {
	// Register ToolRegistry
	c.BindSingleton("ai.tools", func(container *foundation.Container) any {
		return sp.toolRegistry
	})

	// Register PromptRegistry
	c.BindSingleton("ai.prompts", func(container *foundation.Container) any {
		return sp.promptRegistry
	})

	// Register MCPServer
	c.BindSingleton("ai.mcp", func(container *foundation.Container) any {
		return sp.mcpServer
	})

	// Register In-Memory Vector Store for testing and prototyping
	c.BindSingleton("ai.store.memory", func(container *foundation.Container) any {
		return sp.memoryStore
	})
}

// Boot performs lifecycle initialization during the framework boot phase.
func (sp *ServiceProvider) Boot(c *foundation.Container) {}
