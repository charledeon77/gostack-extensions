/*
Package ai provides multi-turn conversation management and context windowing.
*/
package ai

import (
	"encoding/json"
)

// Role represents the participant role in a conversation turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall represents an assistant's invocation of a registered tool.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Message represents a single message in a chat history.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Conversation tracks an ordered sequence of messages with sliding-window truncation.
type Conversation struct {
	messages []Message
}

// NewConversation creates an empty Conversation.
func NewConversation() *Conversation {
	return &Conversation{messages: make([]Message, 0)}
}

// Add appends a message with the given role and text content.
func (c *Conversation) Add(role Role, content string) *Conversation {
	c.messages = append(c.messages, Message{Role: role, Content: content})
	return c
}

// AddSystem appends a system prompt.
func (c *Conversation) AddSystem(content string) *Conversation {
	return c.Add(RoleSystem, content)
}

// AddUser appends a user query.
func (c *Conversation) AddUser(content string) *Conversation {
	return c.Add(RoleUser, content)
}

// AddAssistant appends an assistant reply.
func (c *Conversation) AddAssistant(content string) *Conversation {
	return c.Add(RoleAssistant, content)
}

// AddToolResult appends the output of an executed tool call.
func (c *Conversation) AddToolResult(toolCallID, content string) *Conversation {
	c.messages = append(c.messages, Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
	})
	return c
}

// Messages returns all messages in the conversation.
func (c *Conversation) Messages() []Message {
	return c.messages
}

// Last returns the most recent message, or nil if the conversation is empty.
func (c *Conversation) Last() *Message {
	if len(c.messages) == 0 {
		return nil
	}
	return &c.messages[len(c.messages)-1]
}

// Window returns a trimmed message history honoring a sliding window constraint:
// 1. Any initial System prompt is always preserved at index 0.
// 2. The most recent `lastN` user/assistant turns are kept.
// Older turns are dropped, preventing LLM context window overflows.
func (c *Conversation) Window(lastN int) []Message {
	if lastN <= 0 || len(c.messages) <= lastN {
		return c.messages
	}

	var result []Message

	// Retain initial system message if present
	hasSystem := len(c.messages) > 0 && c.messages[0].Role == RoleSystem
	if hasSystem {
		result = append(result, c.messages[0])
	}

	// Calculate start index for the sliding window
	start := len(c.messages) - lastN
	if hasSystem && start == 0 {
		start = 1
	}

	for i := start; i < len(c.messages); i++ {
		result = append(result, c.messages[i])
	}

	return result
}

// ToJSON serializes the full conversation to JSON.
func (c *Conversation) ToJSON() ([]byte, error) {
	return json.Marshal(c.messages)
}
