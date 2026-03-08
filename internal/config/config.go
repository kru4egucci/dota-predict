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
	SteamAPIKey      string
	OddsPapiAPIKey   string
	TelegramBotToken string
	TelegramChatID   string
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
		SteamAPIKey:      os.Getenv("STEAM_API_KEY"),
		OddsPapiAPIKey:   os.Getenv("ODDSPAPI_API_KEY"),
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
	}, nil
}

// ValidateServer checks that server-mode-specific config is present.
func (c *Config) ValidateServer() error {
	if c.SteamAPIKey == "" {
		return fmt.Errorf("STEAM_API_KEY is required for server mode (Steam API используется для поиска лайв турнирных матчей)")
	}
	if c.TelegramBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required for server mode")
	}
	if c.TelegramChatID == "" {
		return fmt.Errorf("TELEGRAM_CHAT_ID is required for server mode")
	}
	return nil
}
