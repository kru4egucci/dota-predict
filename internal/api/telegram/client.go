package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.telegram.org/bot"

// Client is a simple Telegram Bot API client.
type Client struct {
	httpClient *http.Client
	token      string
	chatID     string
}

// NewClient creates a new Telegram client.
func NewClient(token, chatID string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		chatID:     chatID,
	}
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// SendMessage sends a message to the configured chat. Supports HTML parse mode.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	// Telegram has a 4096 character limit per message.
	// If the text is longer, split into multiple messages.
	const maxLen = 4000
	if len(text) <= maxLen {
		return c.sendOne(ctx, text)
	}

	// Split by newlines, trying not to break mid-line.
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxLen {
			// Find last newline before maxLen.
			cut := maxLen
			for i := maxLen; i > maxLen/2; i-- {
				if text[i] == '\n' {
					cut = i
					break
				}
			}
			chunk = text[:cut]
			text = text[cut:]
		} else {
			text = ""
		}
		if err := c.sendOne(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendOne(ctx context.Context, text string) error {
	body := sendMessageRequest{
		ChatID:    c.chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal telegram request: %w", err)
	}

	url := apiBase + c.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram API error: %s", apiResp.Description)
	}

	return nil
}
