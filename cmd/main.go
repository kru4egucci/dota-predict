package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"dota-predict/internal/analyzer"
	"dota-predict/internal/api/opendota"
	"dota-predict/internal/api/openrouter"
	"dota-predict/internal/collector"
	"dota-predict/internal/config"
	"dota-predict/internal/display"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Использование: %s <match_id>\n", os.Args[0])
		os.Exit(1)
	}

	matchID, err := strconv.ParseInt(os.Args[1], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: некорректный match_id %q: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}

	odClient := opendota.NewClient()
	orClient := openrouter.NewClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel)

	ctx := context.Background()

	coll := collector.New(odClient)
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
