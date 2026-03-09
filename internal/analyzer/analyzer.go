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

const draftSystemPrompt = `Ты — экспертный аналитик драфтов Dota 2 с глубоким знанием матчапов героев, синергий, текущей меты и теории драфта.

Тебе будут предоставлены данные о драфте матча Dota 2: пики героев, их винрейты и матчапы. Твоя задача — оценить силу драфта каждой стороны, полностью игнорируя командные и игроковые факторы. Только герои и их взаимодействия.

Будь аналитичен и точен. Ссылайся на конкретные цифры из данных.

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

var draftResponseFormat = &models.ResponseFormat{
	Type: "json_schema",
	JSONSchema: &models.JSONSchema{
		Name:   "draft_prediction",
		Strict: true,
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"draft_advantage": {"type": "string"},
				"radiant_win_prob": {"type": "number"},
				"dire_win_prob": {"type": "number"},
				"key_factors": {
					"type": "array",
					"items": {"type": "string"}
				},
				"analysis": {"type": "string"}
			},
			"required": ["draft_advantage", "radiant_win_prob", "dire_win_prob", "key_factors", "analysis"],
			"additionalProperties": false
		}`),
	},
}

// Predict sends collected data to the LLM and returns the prediction.
func (a *Analyzer) Predict(ctx context.Context, data *models.CollectedData) (*models.Prediction, error) {
	prompt := buildPrompt(data)
	draftPrompt := buildDraftPrompt(data)

	slog.Info("отправка данных в LLM [4/4]")

	type llmResult struct {
		text string
		err  error
	}

	mainCh := make(chan llmResult, 1)
	draftCh := make(chan llmResult, 1)

	go func() {
		start := time.Now()
		resp, err := a.client.ChatCompletion(ctx, []models.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		}, mainResponseFormat)
		if err != nil {
			slog.Error("ошибка основного LLM анализа", "error", err, "duration", time.Since(start).String())
			mainCh <- llmResult{err: fmt.Errorf("LLM analysis: %w", err)}
			return
		}
		if len(resp.Choices) == 0 {
			slog.Error("LLM вернул пустой ответ (основной анализ)", "duration", time.Since(start).String())
			mainCh <- llmResult{err: fmt.Errorf("LLM returned no response")}
			return
		}
		slog.Info("основной LLM анализ завершён", "duration", time.Since(start).String())
		mainCh <- llmResult{text: resp.Choices[0].Message.Content}
	}()

	go func() {
		start := time.Now()
		resp, err := a.client.ChatCompletion(ctx, []models.ChatMessage{
			{Role: "system", Content: draftSystemPrompt},
			{Role: "user", Content: draftPrompt},
		}, draftResponseFormat)
		if err != nil {
			slog.Warn("ошибка LLM анализа драфта", "error", err, "duration", time.Since(start).String())
			draftCh <- llmResult{err: fmt.Errorf("LLM draft analysis: %w", err)}
			return
		}
		if len(resp.Choices) == 0 {
			slog.Warn("LLM вернул пустой ответ (драфт)", "duration", time.Since(start).String())
			draftCh <- llmResult{err: fmt.Errorf("LLM returned no response for draft")}
			return
		}
		slog.Info("LLM анализ драфта завершён", "duration", time.Since(start).String())
		draftCh <- llmResult{text: resp.Choices[0].Message.Content}
	}()

	mainRes := <-mainCh
	if mainRes.err != nil {
		return nil, mainRes.err
	}

	draftRes := <-draftCh

	draftText := ""
	if draftRes.err == nil {
		draftText = draftRes.text
	} else {
		slog.Warn("анализ драфта не удался, продолжаю без него", "error", draftRes.err)
	}

	parsed := parsePrediction(mainRes.text, draftText)

	prediction := &models.Prediction{
		Analysis:      parsed.Analysis,
		DraftAnalysis: parsed.DraftAnalysis,
		Betting:       parsed.Betting,
		Factors:       parsed.Factors,
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
