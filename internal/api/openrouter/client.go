package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dota-predict/internal/models"
)

const apiURL = "https://openrouter.ai/api/v1/chat/completions"

// Client is an OpenRouter LLM API client.
type Client struct {
	httpClient *http.Client
	apiKey     string
	model      string
}

// NewClient creates a new OpenRouter client.
func NewClient(apiKey, model string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		apiKey:     apiKey,
		model:      model,
	}
}

// ChatCompletion sends messages to the LLM and returns the response.
func (c *Client) ChatCompletion(ctx context.Context, messages []models.ChatMessage) (*models.ChatResponse, error) {
	body := models.ChatRequest{
		Model:    c.model,
		Messages: messages,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/dota-predict")
	req.Header.Set("X-Title", "Dota 2 Match Predictor")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenRouter API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp models.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &chatResp, nil
}
