package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"dota-predict/internal/analyzer"
	"dota-predict/internal/api/oddspapi"
	"dota-predict/internal/api/opendota"
	"dota-predict/internal/api/openrouter"
	"dota-predict/internal/api/steam"
	"dota-predict/internal/api/telegram"
	"dota-predict/internal/collector"
	"dota-predict/internal/config"
	"dota-predict/internal/display"
	"dota-predict/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}

	if os.Args[1] == "server" {
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
		fmt.Fprintf(os.Stderr, "Ошибка: некорректный match_id %q: %v\n", matchIDStr, err)
		os.Exit(1)
	}

	odClient := opendota.NewClient()
	steamClient := steam.NewClient(cfg.SteamAPIKey)
	orClient := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	ctx := context.Background()

	coll := collector.New(odClient, steamClient)
	data, err := coll.CollectMatchData(ctx, matchID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка сбора данных: %v\n", err)
		os.Exit(1)
	}

	ana := analyzer.New(orClient)
	prediction, err := ana.Predict(ctx, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка анализа матча: %v\n", err)
		os.Exit(1)
	}

	display.PrintPrediction(prediction)
}

func runServer(cfg *config.Config) {
	if err := cfg.ValidateServer(); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка конфигурации сервера: %v\n", err)
		os.Exit(1)
	}

	odClient := opendota.NewClient()
	steamClient := steam.NewClient(cfg.SteamAPIKey)
	orClient := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)
	tgClient := telegram.NewClient(cfg.TelegramBotToken, cfg.TelegramChatID)

	var oddsClient *oddspapi.Client
	if cfg.OddsPapiAPIKey != "" {
		oddsClient = oddspapi.NewClient(cfg.OddsPapiAPIKey)
	}

	srv := server.New(odClient, steamClient, orClient, oddsClient, tgClient)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка сервера: %v\n", err)
		os.Exit(1)
	}
}
