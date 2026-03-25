package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"dota-predict/internal/api/openrouter"
	"dota-predict/internal/models"
)

// Analyzer builds prompts and calls the LLM for match prediction.
type Analyzer struct {
	client *openrouter.Client
}

// New creates a new Analyzer.
func New(client *openrouter.Client) *Analyzer {
	return &Analyzer{client: client}
}

const systemPrompt = `Ты — экспертный аналитик Dota 2 и предсказатель матчей с глубоким знанием меты, матчапов героев, командной динамики и профессиональной сцены.

Тебе будут предоставлены подробные статистические данные о матче Dota 2: пики героев, статистика игроков, результаты команд, винрейты матчапов героев и многое другое. Твоя задача — проанализировать все эти данные комплексно и дать обоснованный, основанный на данных прогноз о том, какая команда с большей вероятностью победит.

Будь аналитичен и точен. Ссылайся на конкретные цифры из данных. Учитывай синергии драфта, контрпики, комфорт игроков на героях, форму команд, историю личных встреч и текущую мету.

ВАЖНО: Весь текст в ответе должен быть на русском языке. Ответ — валидный JSON.`

// JSON schemas for structured output.
var mainResponseFormat = &models.ResponseFormat{
	Type: "json_schema",
	JSONSchema: &models.JSONSchema{
		Name:   "match_prediction",
		Strict: true,
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"factors": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"weight": {"type": "integer"},
							"advantage": {"type": "string"},
							"degree": {"type": "string"},
							"reasoning": {"type": "string"}
						},
						"required": ["name", "weight", "advantage", "degree", "reasoning"],
						"additionalProperties": false
					}
				},
				"winner": {"type": "string"},
				"radiant_win_prob": {"type": "number"},
				"dire_win_prob": {"type": "number"},
				"confidence": {"type": "string"},
				"key_factors": {
					"type": "array",
					"items": {"type": "string"}
				},
				"analysis": {"type": "string"}
			},
			"required": ["factors", "winner", "radiant_win_prob", "dire_win_prob", "confidence", "key_factors", "analysis"],
			"additionalProperties": false
		}`),
	},
}

// Predict sends collected data to the LLM and returns the prediction.
func (a *Analyzer) Predict(ctx context.Context, data *models.CollectedData) (*models.Prediction, error) {
	prompt := buildPrompt(data)

	slog.Info("отправка данных в LLM [4/4]")

	start := time.Now()
	resp, err := a.client.ChatCompletion(ctx, []models.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}, mainResponseFormat)
	if err != nil {
		slog.Error("ошибка LLM анализа", "error", err, "duration", time.Since(start).String())
		return nil, fmt.Errorf("LLM analysis: %w", err)
	}
	if len(resp.Choices) == 0 {
		slog.Error("LLM вернул пустой ответ", "duration", time.Since(start).String())
		return nil, fmt.Errorf("LLM returned no response")
	}
	slog.Info("LLM анализ завершён", "duration", time.Since(start).String())

	parsed := parsePrediction(resp.Choices[0].Message.Content)

	prediction := &models.Prediction{
		Analysis: parsed.Analysis,
		Betting:  parsed.Betting,
		Factors:  parsed.Factors,
	}

	if data.RadiantTeam != nil {
		prediction.RadiantTeamName = data.RadiantTeam.Name
	} else if data.Match.RadiantTeam.Name != "" {
		prediction.RadiantTeamName = data.Match.RadiantTeam.Name
	} else {
		prediction.RadiantTeamName = "Radiant"
	}

	if data.DireTeam != nil {
		prediction.DireTeamName = data.DireTeam.Name
	} else if data.Match.DireTeam.Name != "" {
		prediction.DireTeamName = data.Match.DireTeam.Name
	} else {
		prediction.DireTeamName = "Dire"
	}

	slog.Info("прогноз сформирован",
		"radiant", prediction.RadiantTeamName,
		"dire", prediction.DireTeamName,
		"confidence", prediction.Betting.Confidence,
	)

	return prediction, nil
}
