package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"dota-predict/internal/analyzer"
	"dota-predict/internal/api/gc"
	"dota-predict/internal/api/gsheets"
	"dota-predict/internal/api/oddspapi"
	"dota-predict/internal/api/opendota"
	"dota-predict/internal/api/openrouter"
	"dota-predict/internal/api/steam"
	"dota-predict/internal/api/telegram"
	"dota-predict/internal/collector"
	"dota-predict/internal/config"
	"dota-predict/internal/display"
	"dota-predict/internal/logger"
	"dota-predict/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	isServer := os.Args[1] == "server"
	logger.Setup(isServer)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("не удалось загрузить конфигурацию", "error", err)
		os.Exit(1)
	}

	slog.Info("конфигурация загружена", "model", cfg.OpenRouterModel, "mode", os.Args[1])

	if isServer {
		runServer(cfg)
	} else {
		runAnalysis(cfg, os.Args[1])
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Использование:\n")
	fmt.Fprintf(os.Stderr, "  %s <match_id>   — анализ конкретного матча\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s server       — запуск сервера мониторинга тир-1 матчей\n", os.Args[0])
}

func runAnalysis(cfg *config.Config, matchIDStr string) {
	matchID, err := strconv.ParseInt(matchIDStr, 10, 64)
	if err != nil {
		slog.Error("некорректный match_id", "input", matchIDStr, "error", err)
		os.Exit(1)
	}

	log := slog.With("match_id", matchID)

	odClient := opendota.NewClient(cfg.OpenDotaAPIKey, cfg.ProxiedHTTPClient(60*time.Second))
	steamClient := steam.NewClient(cfg.SteamAPIKey, cfg.ProxiedHTTPClient(30*time.Second))
	orClient := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	ctx := context.Background()

	log.Info("начинаю сбор данных")
	coll := collector.New(odClient, steamClient)
	data, err := coll.CollectMatchData(ctx, matchID)
	if err != nil {
		log.Error("ошибка сбора данных", "error", err)
		os.Exit(1)
	}

	log.Info("начинаю анализ")
	ana := analyzer.New(orClient)
	prediction, err := ana.Predict(ctx, data)
	if err != nil {
		log.Error("ошибка анализа матча", "error", err)
		os.Exit(1)
	}

	log.Info("анализ завершён")
	display.PrintPrediction(prediction)
}

func runServer(cfg *config.Config) {
	if err := cfg.ValidateServer(); err != nil {
		slog.Error("ошибка конфигурации сервера", "error", err)
		os.Exit(1)
	}

	odClient := opendota.NewClient(cfg.OpenDotaAPIKey, cfg.ProxiedHTTPClient(60*time.Second))
	steamClient := steam.NewClient(cfg.SteamAPIKey, cfg.ProxiedHTTPClient(30*time.Second))
	orClient := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
	tgClient := telegram.NewClient(cfg.TelegramBotToken, cfg.TelegramChatID)

	var oddsClient *oddspapi.Client
	if cfg.OddsPapiAPIKey != "" {
		oddsClient = oddspapi.NewClient(cfg.OddsPapiAPIKey, cfg.ProxiedHTTPClient(30*time.Second))
	}

	var gsheetsClient *gsheets.Client
	if cfg.GoogleServiceAccountFile != "" {
		spreadsheetID := cfg.GoogleSpreadsheetID
		if spreadsheetID == "" {
			spreadsheetID = "1s1db7iEIpt-2UYlyri_o0ucNf-z3larAHvM_KFES_JI"
		}
		sheetName := cfg.GoogleSheetName
		if sheetName == "" {
			sheetName = "Birmingham 2026"
		}
		var err error
		gsheetsClient, err = gsheets.NewClient(cfg.GoogleServiceAccountFile, spreadsheetID, sheetName)
		if err != nil {
			slog.Error("ошибка инициализации Google Sheets", "error", err)
			// Non-fatal: continue without Sheets integration.
		} else if gsheetsClient != nil {
			slog.Info("Google Sheets интеграция активна", "sheet", sheetName)
		}
	}

	var gcClient *gc.Client
	if cfg.SteamGCUsername != "" {
		gcClient = gc.NewClient(gc.Config{
			Username: cfg.SteamGCUsername,
			Password: cfg.SteamGCPassword,
			AuthCode: cfg.SteamGCAuthCode,
		})
		slog.Info("Game Coordinator fallback активен")
	}

	srv := server.New(odClient, steamClient, orClient, oddsClient, tgClient, gsheetsClient, gcClient)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if gcClient != nil {
		go gcClient.Run(ctx)
	}

	if err := srv.Run(ctx); err != nil {
		slog.Error("ошибка сервера", "error", err)
		os.Exit(1)
	}
}
