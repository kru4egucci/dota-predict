package analyzer

import (
	"context"
	"fmt"

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

ВАЖНО: Весь ответ должен быть на русском языке.`

// Predict sends collected data to the LLM and returns the prediction.
func (a *Analyzer) Predict(ctx context.Context, data *models.CollectedData) (*models.Prediction, error) {
	prompt := buildPrompt(data)

	messages := []models.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	fmt.Println("[4/4] Отправка данных в LLM для анализа...")

	resp, err := a.client.ChatCompletion(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned no response")
	}

	analysis := resp.Choices[0].Message.Content

	prediction := &models.Prediction{
		Analysis: analysis,
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

	return prediction, nil
}
