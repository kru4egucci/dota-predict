package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds application configuration loaded from .env / environment variables.
type Config struct {
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// Load reads configuration from .env file (if present) and environment variables.
// Environment variables take precedence over .env values.
func Load() (*Config, error) {
	_ = godotenv.Load() // ignore error — .env is optional

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is required (set in .env or environment)")
	}

	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = "anthropic/claude-sonnet-4"
	}

	return &Config{
		OpenRouterAPIKey: apiKey,
		OpenRouterModel:  model,
	}, nil
}
