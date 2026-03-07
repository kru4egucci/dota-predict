package display

import (
	"fmt"
	"strings"

	"dota-predict/internal/models"
)

const divider = "============================================================"

// PrintPrediction outputs the prediction result to the console.
func PrintPrediction(p *models.Prediction) {
	fmt.Println()
	fmt.Println(divider)
	fmt.Println(centerText("ПРОГНОЗ МАТЧА DOTA 2", 60))
	fmt.Println(divider)
	fmt.Println()
	fmt.Printf("  %s (Radiant)  vs  %s (Dire)\n", p.RadiantTeamName, p.DireTeamName)
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()
	fmt.Println(p.Analysis)
	fmt.Println()
	fmt.Println(divider)
}

func centerText(s string, width int) string {
	runeCount := len([]rune(s))
	if runeCount >= width {
		return s
	}
	pad := (width - runeCount) / 2
	return strings.Repeat(" ", pad) + s
}
