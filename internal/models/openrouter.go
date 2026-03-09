package models

import "encoding/json"

// ChatRequest is the OpenRouter API request body (OpenAI-compatible).
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Reasoning      *Reasoning      `json:"reasoning,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ChatMessage represents a single message in the chat.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Reasoning configures extended thinking for supported models.
type Reasoning struct {
	Effort string `json:"effort,omitempty"` // "high", "medium", "low"
}

// ResponseFormat specifies the desired output format.
type ResponseFormat struct {
	Type       string      `json:"type"`                  // "json_schema" or "json_object"
	JSONSchema *JSONSchema `json:"json_schema,omitempty"` // required when type = "json_schema"
}

// JSONSchema defines a strict JSON schema for structured output.
type JSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

// ChatResponse is the OpenRouter API response body.
type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is a single completion choice.
type Choice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

// Usage contains token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
