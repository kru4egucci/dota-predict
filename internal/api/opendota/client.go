package opendota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	baseURL   = "https://api.opendota.com/api"
	rateLimit = 60
)

// Client is a rate-limited OpenDota API client.
type Client struct {
	httpClient *http.Client
	limiter    *rateLimiter
}

// NewClient creates a new OpenDota API client with built-in rate limiting.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		limiter:    newRateLimiter(rateLimit, time.Minute),
	}
}

const maxRetries = 3

// get performs a rate-limited GET request with retry on 429 and decodes the JSON response into result.
func (c *Client) get(ctx context.Context, path string, result interface{}) error {
	url := baseURL + path

	for attempt := range maxRetries {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating request for %s: %w", path, err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("requesting %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			backoff := time.Duration(attempt+1) * 10 * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("API %s returned status %d: %s", path, resp.StatusCode, string(body))
		}

		err = json.NewDecoder(resp.Body).Decode(result)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("decoding response from %s: %w", path, err)
		}

		return nil
	}

	return fmt.Errorf("API %s: rate limit exceeded after %d retries", path, maxRetries)
}

// rateLimiter implements a token-bucket rate limiter.
type rateLimiter struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	refillRate time.Duration
	lastRefill time.Time
}

func newRateLimiter(maxRequests int, period time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:     maxRequests,
		maxTokens:  maxRequests,
		refillRate: period,
		lastRefill: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *rateLimiter) Wait(ctx context.Context) error {
	for {
		rl.mu.Lock()
		rl.refill()
		if rl.tokens > 0 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (rl *rateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	if elapsed >= rl.refillRate {
		rl.tokens = rl.maxTokens
		rl.lastRefill = now
	}
}
