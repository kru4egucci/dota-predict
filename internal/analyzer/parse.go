package analyzer

import (
	"regexp"
	"strconv"
	"strings"

	"dota-predict/internal/models"
)

var (
	reRadiantProb    = regexp.MustCompile(`(?i)вероятность победы Radiant[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reDireProb       = regexp.MustCompile(`(?i)вероятность победы Dire[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reConfidence     = regexp.MustCompile(`(?i)уверенность[^:]*:\s*\**\s*(низкая|средняя|высокая)`)
	reDraftRadiant   = regexp.MustCompile(`(?i)вероятность победы Radiant по драфту[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
	reDraftDire      = regexp.MustCompile(`(?i)вероятность победы Dire по драфту[^:]*:\s*\**\s*(\d+(?:[.,]\d+)?)\s*%`)
)

func parseProb(s string) float64 {
	s = strings.Replace(s, ",", ".", 1)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// parsePrediction extracts probabilities and confidence from LLM text output.
func parsePrediction(mainText, draftText string) models.BettingInfo {
	var info models.BettingInfo

	if m := reRadiantProb.FindStringSubmatch(mainText); len(m) > 1 {
		info.RadiantWinProb = parseProb(m[1])
	}
	if m := reDireProb.FindStringSubmatch(mainText); len(m) > 1 {
		info.DireWinProb = parseProb(m[1])
	}
	if m := reConfidence.FindStringSubmatch(mainText); len(m) > 1 {
		info.Confidence = strings.ToLower(m[1])
	}

	if m := reDraftRadiant.FindStringSubmatch(draftText); len(m) > 1 {
		info.DraftRadiantProb = parseProb(m[1])
	}
	if m := reDraftDire.FindStringSubmatch(draftText); len(m) > 1 {
		info.DraftDireProb = parseProb(m[1])
	}

	calcOdds(&info)
	return info
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
