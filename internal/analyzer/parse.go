package analyzer

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"dota-predict/internal/models"
)

// --- JSON response structs (matched by json_schema) ---

type mainAnalysisJSON struct {
	Factors        []factorJSON `json:"factors"`
	Winner         string       `json:"winner"`
	RadiantWinProb float64      `json:"radiant_win_prob"`
	DireWinProb    float64      `json:"dire_win_prob"`
	Confidence     string       `json:"confidence"`
	KeyFactors     []string     `json:"key_factors"`
	Analysis       string       `json:"analysis"`
}

type factorJSON struct {
	Name      string `json:"name"`
	Weight    int    `json:"weight"`
	Advantage string `json:"advantage"`
	Degree    string `json:"degree"`
	Reasoning string `json:"reasoning"`
}

type draftAnalysisJSON struct {
	DraftAdvantage string   `json:"draft_advantage"`
	RadiantWinProb float64  `json:"radiant_win_prob"`
	DireWinProb    float64  `json:"dire_win_prob"`
	KeyFactors     []string `json:"key_factors"`
	Analysis       string   `json:"analysis"`
}

// --- Regex fallback patterns ---

var (
	reRadiantProb  = regexp.MustCompile(`(?i)вероятность победы Radiant[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reDireProb     = regexp.MustCompile(`(?i)вероятность победы Dire[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reConfidence   = regexp.MustCompile(`(?i)уверенность[^:]*:\s*\**\s*(низкая|средняя|высокая)`)
	reDraftRadiant = regexp.MustCompile(`(?i)вероятность победы Radiant по драфту[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reDraftDire    = regexp.MustCompile(`(?i)вероятность победы Dire по драфту[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
)

// parsedResult holds all extracted data from LLM responses.
type parsedResult struct {
	Betting       models.BettingInfo
	Analysis      string
	DraftAnalysis string
	Factors       []models.FactorAssessment
}

// parsePrediction extracts probabilities, confidence, factors, and analysis text from LLM output.
// Tries JSON parsing first (structured output), falls back to regex.
func parsePrediction(mainText, draftText string) parsedResult {
	var r parsedResult

	// --- Main analysis ---
	cleanedMain := extractJSON(mainText)
	var mainResp mainAnalysisJSON
	if err := json.Unmarshal([]byte(cleanedMain), &mainResp); err == nil {
		// JSON parsed successfully — use structured data.
		r.Betting.RadiantWinProb = mainResp.RadiantWinProb
		r.Betting.DireWinProb = mainResp.DireWinProb
		r.Betting.Confidence = strings.ToLower(mainResp.Confidence)
		r.Analysis = formatMainAnalysis(&mainResp)
		for _, f := range mainResp.Factors {
			r.Factors = append(r.Factors, models.FactorAssessment{
				Name:      f.Name,
				Weight:    f.Weight,
				Advantage: f.Advantage,
				Degree:    f.Degree,
				Reasoning: f.Reasoning,
			})
		}
		slog.Debug("parse: основной анализ распарсен как JSON",
			"radiant_prob", r.Betting.RadiantWinProb,
			"dire_prob", r.Betting.DireWinProb,
			"factors", len(r.Factors),
		)
		if mainResp.RadiantWinProb <= 0 {
			slog.Warn("parse: JSON распарсен, но RadiantWinProb <= 0, пробуем regex",
				"radiant_prob", mainResp.RadiantWinProb,
			)
			r.Betting.RadiantWinProb = matchProb(reRadiantProb, mainResp.Analysis)
			r.Betting.DireWinProb = matchProb(reDireProb, mainResp.Analysis)
		}
	} else {
		// Regex fallback.
		slog.Warn("parse: JSON не удался, используем regex", "error", err)
		r.Betting.RadiantWinProb = matchProb(reRadiantProb, mainText)
		r.Betting.DireWinProb = matchProb(reDireProb, mainText)
		if m := reConfidence.FindStringSubmatch(mainText); len(m) > 1 {
			r.Betting.Confidence = strings.ToLower(m[1])
		}
		r.Analysis = mainText
	}

	// --- Draft analysis ---
	if draftText != "" {
		cleanedDraft := extractJSON(draftText)
		var draftResp draftAnalysisJSON
		if err := json.Unmarshal([]byte(cleanedDraft), &draftResp); err == nil {
			r.Betting.DraftRadiantProb = draftResp.RadiantWinProb
			r.Betting.DraftDireProb = draftResp.DireWinProb
			r.DraftAnalysis = draftResp.Analysis
			slog.Debug("parse: драфт распарсен как JSON",
				"radiant_prob", r.Betting.DraftRadiantProb,
				"dire_prob", r.Betting.DraftDireProb,
			)
		} else {
			// Regex fallback.
			r.Betting.DraftRadiantProb = matchProb(reDraftRadiant, draftText)
			r.Betting.DraftDireProb = matchProb(reDraftDire, draftText)
			r.DraftAnalysis = draftText
		}
	}

	calcOdds(&r.Betting)
	return r
}

// formatMainAnalysis builds a readable Markdown text from structured JSON response.
func formatMainAnalysis(resp *mainAnalysisJSON) string {
	var sb strings.Builder

	// Factor assessments.
	sb.WriteString("**Оценка по факторам:**\n\n")
	for _, f := range resp.Factors {
		sb.WriteString("  • **")
		sb.WriteString(f.Name)
		sb.WriteString("** → ")
		sb.WriteString(f.Advantage)
		if !strings.EqualFold(f.Advantage, "Equal") {
			sb.WriteString(", ")
			sb.WriteString(f.Degree)
			sb.WriteString(" преимущество")
		}
		sb.WriteString("\n")
		sb.WriteString("    ")
		sb.WriteString(f.Reasoning)
		sb.WriteString("\n\n")
	}

	// Key factors.
	sb.WriteString("**Ключевые факторы:**\n\n")
	for i, kf := range resp.KeyFactors {
		sb.WriteString("  ")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(kf)
		sb.WriteString("\n")
	}

	// Detailed analysis.
	sb.WriteString("\n**Детальный анализ:**\n\n")
	sb.WriteString(resp.Analysis)

	return sb.String()
}

// extractJSON tries to extract a JSON object from text that may be wrapped
// in markdown code fences (```json ... ```) or contain surrounding text.
func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)

	// Already valid JSON?
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}

	// Try to extract from markdown code fences.
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		start := strings.Index(trimmed[idx+3:], "\n")
		if start >= 0 {
			inner := trimmed[idx+3+start+1:]
			if end := strings.Index(inner, "```"); end >= 0 {
				return strings.TrimSpace(inner[:end])
			}
		}
	}

	// Try to find first { ... last }.
	if first := strings.Index(trimmed, "{"); first >= 0 {
		if last := strings.LastIndex(trimmed, "}"); last > first {
			return trimmed[first : last+1]
		}
	}

	return text
}

// matchProb extracts a probability value using a regex pattern.
func matchProb(re *regexp.Regexp, text string) float64 {
	m := re.FindStringSubmatch(text)
	if len(m) > 1 {
		return parseProb(m[1])
	}
	return 0
}

func parseProb(s string) float64 {
	s = strings.Replace(s, ",", ".", 1)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func calcOdds(info *models.BettingInfo) {
	margin := confidenceMargin(info.Confidence)

	if info.RadiantWinProb > 0 {
		fair := 100.0 / info.RadiantWinProb
		info.RadiantMinOdds = fair
		info.RadiantComfortOdds = fair * (1 + margin)
	}
	if info.DireWinProb > 0 {
		fair := 100.0 / info.DireWinProb
		info.DireMinOdds = fair
		info.DireComfortOdds = fair * (1 + margin)
	}
}

// confidenceMargin returns the margin to add on top of fair odds.
// Lower confidence = higher margin required for a comfortable bet.
func confidenceMargin(confidence string) float64 {
	switch confidence {
	case "высокая":
		return 0.05 // +5%
	case "средняя":
		return 0.12 // +12%
	default: // низкая or unknown
		return 0.20 // +20%
	}
}
