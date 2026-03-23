package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Config holds application configuration loaded from .env / environment variables.
type Config struct {
	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenDotaAPIKey   string
	SteamAPIKey      string
	OddsPapiAPIKey   string
	TelegramBotToken string
	TelegramChatID           string
	ProxyURL                 string
	GoogleServiceAccountFile string
	GoogleSpreadsheetID      string
	GoogleSheetName          string
	SteamGCUsername          string
	SteamGCPassword          string
	SteamGCAuthCode          string
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
		OpenDotaAPIKey:   os.Getenv("OPENDOTA_API_KEY"),
		SteamAPIKey:      os.Getenv("STEAM_API_KEY"),
		OddsPapiAPIKey:   os.Getenv("ODDSPAPI_API_KEY"),
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		ProxyURL:                 os.Getenv("PROXY_URL"),
		GoogleServiceAccountFile: os.Getenv("GOOGLE_SERVICE_ACCOUNT_FILE"),
		GoogleSpreadsheetID:      os.Getenv("GOOGLE_SPREADSHEET_ID"),
		GoogleSheetName:          os.Getenv("GOOGLE_SHEET_NAME"),
		SteamGCUsername:          os.Getenv("STEAM_GC_USERNAME"),
		SteamGCPassword:          os.Getenv("STEAM_GC_PASSWORD"),
		SteamGCAuthCode:          os.Getenv("STEAM_GC_AUTH_CODE"),
	}, nil
}

// ProxiedHTTPClient returns an *http.Client that routes through ProxyURL if set.
// Falls back to a default client with the given timeout if no proxy is configured.
func (c *Config) ProxiedHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.ProxyURL != "" {
		proxyURL, err := url.Parse(c.ProxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
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
