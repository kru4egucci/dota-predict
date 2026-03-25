package opendota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	baseURL   = "https://api.opendota.com/api"
	rateLimit = 60
)

// Client is a rate-limited OpenDota API client with in-memory caching.
type Client struct {
	httpClient *http.Client
	limiter    *rateLimiter
	apiKey     string
	cache      *cache
}

// NewClient creates a new OpenDota API client with built-in rate limiting.
// If apiKey is provided, it is appended to all requests (paid plan, higher limits).
// If httpClient is provided, it is used for all requests (e.g. for proxy support);
// otherwise a default client with 60s timeout is created.
func NewClient(apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		limiter:    newRateLimiter(rateLimit, time.Minute),
		apiKey:     apiKey,
		cache:      newCache(),
	}
}

const maxRetries = 3

// get performs a rate-limited GET request with retry on 429 and decodes the JSON response into result.
func (c *Client) get(ctx context.Context, path string, result interface{}) error {
	url := baseURL + path
	if c.apiKey != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		url += sep + "api_key=" + c.apiKey
	}

	for attempt := range maxRetries {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating request for %s: %w", path, err)
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			slog.Error("opendota: ошибка HTTP запроса",
				"path", path,
				"attempt", attempt+1,
				"duration", elapsed.String(),
				"error", err,
			)
			return fmt.Errorf("requesting %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			backoff := time.Duration(attempt+1) * 10 * time.Second
			slog.Warn("opendota: rate limit (429), ожидание",
				"path", path,
				"attempt", attempt+1,
				"backoff", backoff.String(),
			)
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
			slog.Error("opendota: неуспешный HTTP статус",
				"path", path,
				"status", resp.StatusCode,
				"body", string(body),
				"duration", elapsed.String(),
			)
			return fmt.Errorf("API %s returned status %d: %s", path, resp.StatusCode, string(body))
		}

		err = json.NewDecoder(resp.Body).Decode(result)
		resp.Body.Close()
		if err != nil {
			slog.Error("opendota: ошибка декодирования JSON",
				"path", path,
				"duration", elapsed.String(),
				"error", err,
			)
			return fmt.Errorf("decoding response from %s: %w", path, err)
		}

		slog.Debug("opendota: запрос выполнен",
			"path", path,
			"status", resp.StatusCode,
			"duration", elapsed.String(),
		)
		return nil
	}

	slog.Error("opendota: все попытки исчерпаны",
		"path", path,
		"max_retries", maxRetries,
	)
	return fmt.Errorf("API %s: rate limit exceeded after %d retries", path, maxRetries)
}

// rateLimiter implements a token-bucket rate limiter using a ticker
// to refill tokens, avoiding spin-wait CPU overhead.
type rateLimiter struct {
	tokens chan struct{}
}

func newRateLimiter(maxRequests int, period time.Duration) *rateLimiter {
	rl := &rateLimiter{
		tokens: make(chan struct{}, maxRequests),
	}
	// Fill initial tokens.
	for range maxRequests {
		rl.tokens <- struct{}{}
	}
	// Refill tokens evenly over the period.
	interval := period / time.Duration(maxRequests)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case rl.tokens <- struct{}{}:
			default: // bucket full
			}
		}
	}()
	return rl
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *rateLimiter) Wait(ctx context.Context) error {
	select {
	case <-rl.tokens:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
