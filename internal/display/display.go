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

	printBettingSection(p)

	fmt.Println(divider)
}

func printBettingSection(p *models.Prediction) {
	b := &p.Betting
	if b.RadiantWinProb == 0 && b.DireWinProb == 0 {
		return
	}

	fmt.Println(divider)
	fmt.Println(centerText("АНАЛИТИКА СТАВОК", 60))
	fmt.Println(divider)
	fmt.Println()

	confidence := b.Confidence
	if confidence == "" {
		confidence = "не определена"
	}
	fmt.Printf("  Уверенность в прогнозе: %s\n\n", strings.ToUpper(confidence))

	fmt.Println("  Общий прогноз (все факторы):")
	fmt.Printf("    %-20s  Вероятность: %5.1f%%\n", p.RadiantTeamName, b.RadiantWinProb)
	fmt.Printf("    %-20s  Вероятность: %5.1f%%\n", p.DireTeamName, b.DireWinProb)
	fmt.Println()

	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()
	fmt.Println("  Рекомендуемые коэффициенты для ставок:")
	fmt.Println()
	fmt.Printf("  %-22s  Мин. кэф   Комфортный кэф\n", "Команда")
	fmt.Printf("  %-22s  --------   --------------\n", strings.Repeat("-", 22))

	if b.RadiantMinOdds > 0 {
		fmt.Printf("  %-22s  %6.2f     %6.2f\n", p.RadiantTeamName, b.RadiantMinOdds, b.RadiantComfortOdds)
	}
	if b.DireMinOdds > 0 {
		fmt.Printf("  %-22s  %6.2f     %6.2f\n", p.DireTeamName, b.DireMinOdds, b.DireComfortOdds)
	}

	fmt.Println()
	fmt.Println("  Мин. кэф — минимальный коэффициент (fair value, точка безубыточности)")
	fmt.Println("  Комфортный кэф — с запасом прочности по уверенности прогноза")
	fmt.Println()
}

func centerText(s string, width int) string {
	runeCount := len([]rune(s))
	if runeCount >= width {
		return s
	}
	pad := (width - runeCount) / 2
	return strings.Repeat(" ", pad) + s
}
