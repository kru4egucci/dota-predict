package analyzer

import (
	"fmt"
	"strings"
	"time"

	"dota-predict/internal/models"
)

func buildPrompt(data *models.CollectedData) string {
	var sb strings.Builder

	sb.WriteString("=== ЗАПРОС НА АНАЛИЗ МАТЧА DOTA 2 ===\n\n")

	writeMatchOverview(&sb, data)
	writeDraftSection(&sb, data)
	writeHeroStatsSection(&sb, data)
	writeMatchupSection(&sb, data)
	writeTeamStatsSection(&sb, data)
	writeTeamFormSection(&sb, data)
	writeH2HSection(&sb, data)
	writeTeamHeroSection(&sb, data)
	writePlayerHeroSection(&sb, data)
	writePlayerFormSection(&sb, data)
	writeAnalysisInstructions(&sb)

	return sb.String()
}

func writeMatchOverview(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 1. ОБЗОР МАТЧА\n")

	radiantName := teamName(data.RadiantTeam, data.Match.RadiantTeam.Name, "Radiant")
	direName := teamName(data.DireTeam, data.Match.DireTeam.Name, "Dire")

	sb.WriteString(fmt.Sprintf("ID матча: %d\n", data.Match.MatchID))
	sb.WriteString(fmt.Sprintf("Radiant: %s\n", radiantName))
	sb.WriteString(fmt.Sprintf("Dire: %s\n\n", direName))
}

func writeDraftSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 2. ДРАФТ (ПИКИ ГЕРОЕВ)\n")

	var radiant, dire []string
	for _, p := range data.Match.Players {
		heroName := heroName(data.HeroNames, p.HeroID)
		playerName := playerDisplayName(p)
		entry := fmt.Sprintf("%s (%s)", heroName, playerName)
		if p.IsRadiant {
			radiant = append(radiant, entry)
		} else {
			dire = append(dire, entry)
		}
	}

	sb.WriteString("Пики Radiant: " + strings.Join(radiant, ", ") + "\n")
	sb.WriteString("Пики Dire: " + strings.Join(dire, ", ") + "\n")

	if len(data.Match.PicksBans) > 0 {
		var radiantBans, direBans []string
		for _, pb := range data.Match.PicksBans {
			if pb.IsPick {
				continue
			}
			name := heroName(data.HeroNames, pb.HeroID)
			if pb.Team == 0 {
				radiantBans = append(radiantBans, name)
			} else {
				direBans = append(direBans, name)
			}
		}
		if len(radiantBans) > 0 {
			sb.WriteString("Баны Radiant: " + strings.Join(radiantBans, ", ") + "\n")
		}
		if len(direBans) > 0 {
			sb.WriteString("Баны Dire: " + strings.Join(direBans, ", ") + "\n")
		}
	}

	sb.WriteString("\n")
}

func writeHeroStatsSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 3. ВИНРЕЙТЫ ГЕРОЕВ (Текущая мета)\n")

	for _, p := range data.Match.Players {
		name := heroName(data.HeroNames, p.HeroID)
		stats := data.HeroStats[p.HeroID]
		if stats == nil {
			sb.WriteString(fmt.Sprintf("  %s: нет данных\n", name))
			continue
		}

		totalPick := stats.Bracket6Pick + stats.Bracket7Pick + stats.Bracket8Pick
		totalWin := stats.Bracket6Win + stats.Bracket7Win + stats.Bracket8Win
		winRate := safePercent(totalWin, totalPick)

		proWinRate := safePercent(stats.ProWin, stats.ProPick)

		side := "Radiant"
		if !p.IsRadiant {
			side = "Dire"
		}

		sb.WriteString(fmt.Sprintf("  [%s] %s: %.1f%% винрейт в пабе (высокие ранги, %d игр), %.1f%% винрейт в про (%d игр)\n",
			side, name, winRate, totalPick, proWinRate, stats.ProPick))
	}

	sb.WriteString("\n")
}

func writeMatchupSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 4. АНАЛИЗ МАТЧАПОВ (Контрпики)\n")

	radiantHeroes, direHeroes := heroIDsByTeam(data)

	sb.WriteString("Герои Radiant против героев Dire:\n")
	for _, rHero := range radiantHeroes {
		matchups := data.HeroMatchups[rHero]
		rName := heroName(data.HeroNames, rHero)
		for _, dHero := range direHeroes {
			dName := heroName(data.HeroNames, dHero)
			for _, mu := range matchups {
				if mu.HeroID == dHero && mu.GamesPlayed > 0 {
					wr := safePercent(mu.Wins, mu.GamesPlayed)
					sb.WriteString(fmt.Sprintf("  %s vs %s: %.1f%% (%d игр)\n",
						rName, dName, wr, mu.GamesPlayed))
				}
			}
		}
	}

	sb.WriteString("\nГерои Dire против героев Radiant:\n")
	for _, dHero := range direHeroes {
		matchups := data.HeroMatchups[dHero]
		dName := heroName(data.HeroNames, dHero)
		for _, rHero := range radiantHeroes {
			rName := heroName(data.HeroNames, rHero)
			for _, mu := range matchups {
				if mu.HeroID == rHero && mu.GamesPlayed > 0 {
					wr := safePercent(mu.Wins, mu.GamesPlayed)
					sb.WriteString(fmt.Sprintf("  %s vs %s: %.1f%% (%d игр)\n",
						dName, rName, wr, mu.GamesPlayed))
				}
			}
		}
	}

	sb.WriteString("\n")
}

func writeTeamStatsSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 5. ОБЩАЯ СТАТИСТИКА КОМАНД\n")

	writeTeamStat(sb, "Radiant", data.RadiantTeam)
	writeTeamStat(sb, "Dire", data.DireTeam)

	sb.WriteString("\n")
}

func writeTeamStat(sb *strings.Builder, side string, team *models.Team) {
	if team == nil {
		sb.WriteString(fmt.Sprintf("  %s: нет данных о команде\n", side))
		return
	}

	total := team.Wins + team.Losses
	wr := safePercent(team.Wins, total)
	sb.WriteString(fmt.Sprintf("  %s — %s: %d-%d (%.1f%%), рейтинг: %.0f\n",
		side, team.Name, team.Wins, team.Losses, wr, team.Rating))
}

func writeTeamFormSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 6. ФОРМА КОМАНД (Последние 10 матчей)\n")

	writeTeamForm(sb, "Radiant", data.RadiantTeam, data.RadiantTeamMatches)
	writeTeamForm(sb, "Dire", data.DireTeam, data.DireTeamMatches)

	sb.WriteString("\n")
}

func writeTeamForm(sb *strings.Builder, side string, team *models.Team, matches []models.TeamMatch) {
	if team == nil || len(matches) == 0 {
		sb.WriteString(fmt.Sprintf("  %s: нет данных о последних матчах\n", side))
		return
	}

	limit := 10
	if len(matches) < limit {
		limit = len(matches)
	}
	recent := matches[:limit]

	wins, losses := countTeamWL(recent)
	sb.WriteString(fmt.Sprintf("  %s — %s: %d-%d в последних %d матчах\n",
		side, team.Name, wins, losses, limit))

	for _, m := range recent {
		won := teamWon(m)
		result := "П"
		if !won {
			result = "Л"
		}
		ago := time.Since(time.Unix(m.StartTime, 0)).Truncate(time.Hour)
		sb.WriteString(fmt.Sprintf("    [%s] vs %s (%s назад)\n", result, m.OpposingTeamName, ago))
	}
}

func writeH2HSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 7. ИСТОРИЯ ЛИЧНЫХ ВСТРЕЧ\n")

	if len(data.HeadToHead) == 0 {
		sb.WriteString("  История личных встреч не найдена.\n\n")
		return
	}

	radiantName := "Radiant"
	direName := "Dire"
	if data.RadiantTeam != nil {
		radiantName = data.RadiantTeam.Name
	}
	if data.DireTeam != nil {
		direName = data.DireTeam.Name
	}

	wins := 0
	for _, m := range data.HeadToHead {
		if teamWon(m) {
			wins++
		}
	}
	losses := len(data.HeadToHead) - wins

	sb.WriteString(fmt.Sprintf("  %s vs %s: всего %d матчей (%s %d - %d %s)\n",
		radiantName, direName, len(data.HeadToHead), radiantName, wins, losses, direName))

	limit := 5
	if len(data.HeadToHead) < limit {
		limit = len(data.HeadToHead)
	}
	sb.WriteString(fmt.Sprintf("  Последние %d встреч:\n", limit))
	for _, m := range data.HeadToHead[:limit] {
		won := teamWon(m)
		result := "П"
		if !won {
			result = "Л"
		}
		sb.WriteString(fmt.Sprintf("    [%s для %s] vs %s (матч %d)\n",
			result, radiantName, m.OpposingTeamName, m.MatchID))
	}

	sb.WriteString("\n")
}

func writeTeamHeroSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 8. ВИНРЕЙТЫ КОМАНД НА КОНКРЕТНЫХ ГЕРОЯХ\n")

	radiantHeroes, direHeroes := heroIDsByTeam(data)
	writeTeamHeroStats(sb, "Radiant", data.RadiantTeam, data.RadiantTeamHeroes, radiantHeroes, data.HeroNames)
	writeTeamHeroStats(sb, "Dire", data.DireTeam, data.DireTeamHeroes, direHeroes, data.HeroNames)

	sb.WriteString("\n")
}

func writeTeamHeroStats(sb *strings.Builder, side string, team *models.Team, teamHeroes []models.TeamHero, pickedIDs []int, heroNames map[int]string) {
	if team == nil || len(teamHeroes) == 0 {
		sb.WriteString(fmt.Sprintf("  %s: нет данных\n", side))
		return
	}

	sb.WriteString(fmt.Sprintf("  %s — %s:\n", side, team.Name))

	heroMap := make(map[int]*models.TeamHero, len(teamHeroes))
	for i := range teamHeroes {
		heroMap[teamHeroes[i].HeroID] = &teamHeroes[i]
	}

	for _, hID := range pickedIDs {
		name := heroName(heroNames, hID)
		th := heroMap[hID]
		if th == nil || th.GamesPlayed == 0 {
			sb.WriteString(fmt.Sprintf("    %s: команда не играла этим героем\n", name))
			continue
		}
		wr := safePercent(th.Wins, th.GamesPlayed)
		sb.WriteString(fmt.Sprintf("    %s: %d игр, %.1f%% винрейт\n", name, th.GamesPlayed, wr))
	}
}

func writePlayerHeroSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 9. КОМФОРТ ИГРОКОВ НА ГЕРОЯХ\n")

	for _, p := range data.Match.Players {
		if p.AccountID == 0 {
			continue
		}

		hName := heroName(data.HeroNames, p.HeroID)
		pName := playerDisplayName(p)
		side := "Radiant"
		if !p.IsRadiant {
			side = "Dire"
		}

		stat := data.PlayerHeroStats[p.AccountID]
		if stat == nil || stat.Games == 0 {
			sb.WriteString(fmt.Sprintf("  [%s] %s на %s: нет данных\n", side, pName, hName))
			continue
		}

		wr := safePercent(stat.Win, stat.Games)
		sb.WriteString(fmt.Sprintf("  [%s] %s на %s: %d игр, %.1f%% винрейт\n",
			side, pName, hName, stat.Games, wr))
	}

	sb.WriteString("\n")
}

func writePlayerFormSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 10. ФОРМА ИГРОКОВ (Последние 20 матчей)\n")

	for _, p := range data.Match.Players {
		if p.AccountID == 0 {
			continue
		}

		pName := playerDisplayName(p)
		side := "Radiant"
		if !p.IsRadiant {
			side = "Dire"
		}

		recent := data.PlayerRecent[p.AccountID]
		if len(recent) == 0 {
			sb.WriteString(fmt.Sprintf("  [%s] %s: нет данных\n", side, pName))
			continue
		}

		limit := 20
		if len(recent) < limit {
			limit = len(recent)
		}
		matches := recent[:limit]

		wins := 0
		totalKills, totalDeaths, totalAssists := 0, 0, 0
		for _, m := range matches {
			isRadiant := m.PlayerSlot < 128
			if (isRadiant && m.RadiantWin) || (!isRadiant && !m.RadiantWin) {
				wins++
			}
			totalKills += m.Kills
			totalDeaths += m.Deaths
			totalAssists += m.Assists
		}

		avgKDA := fmt.Sprintf("%.1f/%.1f/%.1f",
			float64(totalKills)/float64(limit),
			float64(totalDeaths)/float64(limit),
			float64(totalAssists)/float64(limit))

		wr := safePercent(wins, limit)
		sb.WriteString(fmt.Sprintf("  [%s] %s: %d-%d (%.1f%%), средний KDA: %s\n",
			side, pName, wins, limit-wins, wr, avgKDA))
	}

	sb.WriteString("\n")
}

func writeAnalysisInstructions(sb *strings.Builder) {
	sb.WriteString(`=== ИНСТРУКЦИИ ПО АНАЛИЗУ ===
На основе ВСЕХ данных выше предоставь комплексный прогноз матча Dota 2. Проанализируй:

1. **Преимущество драфта**: У какой команды лучше матчапы героев? Учти контрпики, синергии героев внутри каждой команды и как герои масштабируются на разных стадиях игры.
2. **Сила команд**: У какой команды лучше общий винрейт, рейтинг и текущая форма?
3. **Личные встречи**: Что говорит история матчей между этими командами?
4. **Комфорт игроков**: Играют ли игроки на своих комфортных/сигнатурных героях? Насколько они опытны на этих пиках?
5. **Мета героев**: Сильны ли выбранные герои в текущей мете (высокие винрейты)?
6. **Командный опыт на героях**: Насколько хорошо каждая команда выступает на своих конкретных пиках?

Структурируй ответ следующим образом:

**Прогноз победителя:** [Название команды]
**Вероятность победы Radiant:** [X]%
**Вероятность победы Dire:** [Y]%
**Уверенность:** [Низкая/Средняя/Высокая]

**Ключевые факторы:**
1. [Самый важный фактор]
2. [Второй фактор]
3. [Третий фактор]
4. [Четвёртый фактор, если применимо]
5. [Пятый фактор, если применимо]

**Сильные стороны Radiant:**
- [сила 1]
- [сила 2]

**Слабые стороны Radiant:**
- [слабость 1]
- [слабость 2]

**Сильные стороны Dire:**
- [сила 1]
- [сила 2]

**Слабые стороны Dire:**
- [слабость 1]
- [слабость 2]

**Детальный анализ:**
[2-3 абзаца глубокого анализа со ссылками на конкретные данные]
`)
}

// --- Helpers ---

func heroName(names map[int]string, heroID int) string {
	if name, ok := names[heroID]; ok {
		return name
	}
	return fmt.Sprintf("Hero#%d", heroID)
}

func teamName(team *models.Team, matchName, fallback string) string {
	if team != nil && team.Name != "" {
		return team.Name
	}
	if matchName != "" {
		return matchName
	}
	return fallback
}

func playerDisplayName(p models.MatchPlayer) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Personaname != "" {
		return p.Personaname
	}
	return fmt.Sprintf("Player#%d", p.AccountID)
}

func heroIDsByTeam(data *models.CollectedData) (radiant, dire []int) {
	for _, p := range data.Match.Players {
		if p.IsRadiant {
			radiant = append(radiant, p.HeroID)
		} else {
			dire = append(dire, p.HeroID)
		}
	}
	return
}

func safePercent(wins, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(wins) / float64(total) * 100
}

func teamWon(m models.TeamMatch) bool {
	return (m.Radiant && m.RadiantWin) || (!m.Radiant && !m.RadiantWin)
}

func countTeamWL(matches []models.TeamMatch) (wins, losses int) {
	for _, m := range matches {
		if teamWon(m) {
			wins++
		} else {
			losses++
		}
	}
	return
}
