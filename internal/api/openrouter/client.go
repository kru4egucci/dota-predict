package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"dota-predict/internal/models"
)

const apiURL = "https://openrouter.ai/api/v1/chat/completions"

// Client is an OpenRouter LLM API client.
type Client struct {
	httpClient  *http.Client
	apiKey      string
	model       string
	temperature float64
}

// NewClient creates a new OpenRouter client.
func NewClient(apiKey, model string) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: 300 * time.Second},
		apiKey:      apiKey,
		model:       model,
		temperature: 0.3,
	}
}

// ChatCompletion sends messages to the LLM and returns the response.
// If responseFormat is non-nil, the model is instructed to return structured output.
func (c *Client) ChatCompletion(ctx context.Context, messages []models.ChatMessage, responseFormat *models.ResponseFormat) (*models.ChatResponse, error) {
	temp := c.temperature
	maxTokens := 4096

	body := models.ChatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		Reasoning:   &models.Reasoning{Effort: "high"},
	}

	if responseFormat != nil {
		body.ResponseFormat = responseFormat
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	slog.Debug("openrouter: отправка запроса",
		"model", c.model,
		"messages_count", len(messages),
		"request_size", len(jsonBody),
		"temperature", temp,
		"has_response_format", responseFormat != nil,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/dota-predict")
	req.Header.Set("X-Title", "Dota 2 Match Predictor")

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("openrouter: ошибка HTTP запроса",
			"model", c.model,
			"duration", elapsed.String(),
			"error", err,
		)
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("openrouter: неуспешный HTTP статус",
			"model", c.model,
			"status", resp.StatusCode,
			"body", string(respBody),
			"duration", elapsed.String(),
		)
		return nil, fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp models.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		slog.Error("openrouter: ошибка декодирования ответа",
			"model", c.model,
			"duration", elapsed.String(),
			"error", err,
		)
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	responseLen := 0
	if len(chatResp.Choices) > 0 {
		responseLen = len(chatResp.Choices[0].Message.Content)
	}

	slog.Debug("openrouter: ответ получен",
		"model", c.model,
		"status", resp.StatusCode,
		"duration", elapsed.String(),
		"choices", len(chatResp.Choices),
		"response_length", responseLen,
		"prompt_tokens", chatResp.Usage.PromptTokens,
		"completion_tokens", chatResp.Usage.CompletionTokens,
	)

	return &chatResp, nil
}
