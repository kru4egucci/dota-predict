package logger

import (
	"log/slog"
	"os"
)

// Setup initializes the global slog logger.
// Server mode uses JSON format (for log aggregation), CLI mode uses text format.
func Setup(serverMode bool) {
	var handler slog.Handler

	if serverMode {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	slog.SetDefault(slog.New(handler))
}
