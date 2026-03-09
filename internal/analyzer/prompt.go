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
	writeLeagueMetaSection(&sb, data)
	writeMatchupSection(&sb, data)
	writeLaneMatchupSection(&sb, data)
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
	patchLabel := "Текущая мета"
	if data.Patch != "" {
		patchLabel = fmt.Sprintf("Патч %s", data.Patch)
	}
	sb.WriteString(fmt.Sprintf("## 3. СТАТИСТИКА ГЕРОЕВ (%s)\n", patchLabel))

	for _, p := range data.Match.Players {
		name := heroName(data.HeroNames, p.HeroID)
		side := "Radiant"
		if !p.IsRadiant {
			side = "Dire"
		}

		stats := data.HeroStats[p.HeroID]
		if stats == nil {
			sb.WriteString(fmt.Sprintf("  [%s] %s: нет данных\n", side, name))
			continue
		}

		totalPick := stats.Bracket6Pick + stats.Bracket7Pick + stats.Bracket8Pick
		totalWin := stats.Bracket6Win + stats.Bracket7Win + stats.Bracket8Win
		pubWR := safePercent(totalWin, totalPick)
		proWR := safePercent(stats.ProWin, stats.ProPick)

		sb.WriteString(fmt.Sprintf("  [%s] %s:\n", side, name))
		sb.WriteString(fmt.Sprintf("    Паб (высокие ранги): %.1f%% (%d игр)\n", pubWR, totalPick))
		sb.WriteString(fmt.Sprintf("    Про (все время): %.1f%% (%d игр)\n", proWR, stats.ProPick))

		// Patch-specific win rate.
		if ps := data.HeroPatchStats[p.HeroID]; ps != nil && ps.Games > 0 {
			patchWR := safePercent(ps.Wins, ps.Games)
			sb.WriteString(fmt.Sprintf("    Патч %s: %.1f%% (%d игр)\n", data.Patch, patchWR, ps.Games))
		}

		// League-specific stats for this hero.
		if ls := data.HeroLeagueStats[p.HeroID]; ls != nil && ls.Picks > 0 {
			leagueWR := safePercent(ls.Wins, ls.Picks)
			sb.WriteString(fmt.Sprintf("    Турнир: %d пиков, %d банов, %.1f%% винрейт\n",
				ls.Picks, ls.Bans, leagueWR))
		}
	}

	sb.WriteString("\n")
}

func writeLeagueMetaSection(sb *strings.Builder, data *models.CollectedData) {
	if len(data.HeroLeagueStats) == 0 {
		return
	}

	sb.WriteString("## 4. МЕТА ТУРНИРА (Самые контестируемые герои)\n")

	// Sort by contest rate (picks + bans) descending.
	type entry struct {
		heroID  int
		picks   int
		bans    int
		wins    int
		contest int
	}
	entries := make([]entry, 0, len(data.HeroLeagueStats))
	totalMatches := 0
	for _, ls := range data.HeroLeagueStats {
		c := ls.Picks + ls.Bans
		entries = append(entries, entry{
			heroID: ls.HeroID, picks: ls.Picks, bans: ls.Bans,
			wins: ls.Wins, contest: c,
		})
		// Estimate total matches: max picks for a single hero can't exceed total matches.
		if ls.Picks > totalMatches {
			totalMatches = ls.Picks
		}
	}

	// Sort descending by contest count.
	for i := range entries {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].contest > entries[i].contest {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	limit := 15
	if len(entries) < limit {
		limit = len(entries)
	}

	for _, e := range entries[:limit] {
		name := heroName(data.HeroNames, e.heroID)
		wr := safePercent(e.wins, e.picks)
		sb.WriteString(fmt.Sprintf("  %s: %d пиков (%.1f%% WR), %d банов — %d contest\n",
			name, e.picks, wr, e.bans, e.contest))
	}

	sb.WriteString("\n")
}

func writeMatchupSection(sb *strings.Builder, data *models.CollectedData) {
	sb.WriteString("## 5. АНАЛИЗ МАТЧАПОВ (Контрпики)\n")

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
	sb.WriteString("## 7. ОБЩАЯ СТАТИСТИКА КОМАНД\n")

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
	sb.WriteString("## 8. ФОРМА КОМАНД (Последние 10 матчей)\n")

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
	sb.WriteString("## 9. ИСТОРИЯ ЛИЧНЫХ ВСТРЕЧ\n")

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
	sb.WriteString("## 10. ВИНРЕЙТЫ КОМАНД НА КОНКРЕТНЫХ ГЕРОЯХ\n")

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
	sb.WriteString("## 11. КОМФОРТ ИГРОКОВ НА ГЕРОЯХ\n")

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
	sb.WriteString("## 12. ФОРМА ИГРОКОВ (Последние 20 матчей)\n")

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

func buildDraftPrompt(data *models.CollectedData) string {
	var sb strings.Builder

	sb.WriteString("=== ЗАПРОС НА АНАЛИЗ ДРАФТА DOTA 2 ===\n\n")

	writeDraftSection(&sb, data)
	writeHeroStatsSection(&sb, data)
	writeLeagueMetaSection(&sb, data)
	writeMatchupSection(&sb, data)
	writeLaneMatchupSection(&sb, data)
	writeDraftAnalysisInstructions(&sb)

	return sb.String()
}

func writeDraftAnalysisInstructions(sb *strings.Builder) {
	sb.WriteString(`=== ИНСТРУКЦИИ ПО АНАЛИЗУ ===
На основе данных выше проведи ЧИСТЫЙ анализ драфта. Оцени ТОЛЬКО:

1. Матчапы героев: Кто кого контрит? У какой стороны лучше индивидуальные матчапы?
2. Синергии внутри команды: Какие комбо героев есть у каждой стороны? Насколько хорошо герои дополняют друг друга?
3. Мета патча и турнира: Насколько сильны выбранные герои в текущем патче и на данном турнире?
4. Лейн-матчапы: У кого преимущество на миде? Какая сторона доминирует на сайд-лайнах?
5. Масштабирование: Какой драфт сильнее на разных стадиях игры (ранняя/средняя/поздняя)?

ВАЖНО: Полностью ИГНОРИРУЙ силу команд, рейтинги, форму игроков, историю встреч и любые другие факторы кроме самих героев. Это чистый анализ драфта.

=== ФОРМАТ ОТВЕТА (JSON) ===
Ответь валидным JSON-объектом с полями:
- "draft_advantage": "Radiant", "Dire" или "Equal"
- "radiant_win_prob": вероятность победы Radiant по драфту (число 0-100)
- "dire_win_prob": вероятность победы Dire по драфту (число 0-100)
- "key_factors": массив из 3 строк — ключевые факторы драфта с конкретными цифрами
- "analysis": детальный анализ драфта (1-2 абзаца, Markdown-разметка, ссылки на данные)
`)
}

func writeAnalysisInstructions(sb *strings.Builder) {
	sb.WriteString(`=== ИНСТРУКЦИИ ПО АНАЛИЗУ ===
На основе ВСЕХ данных выше предоставь комплексный прогноз матча Dota 2.

ВАЖНО: Каждый фактор имеет фиксированный вес в итоговой оценке. Ты ОБЯЗАН оценить каждый фактор отдельно (кто выигрывает и насколько), а затем вычислить итоговую вероятность как взвешенную сумму.

=== ВЕСА ФАКТОРОВ ===
1. Сила и форма команд (25%): Рейтинг, общий W/L, форма в последних 10 матчах. Если одна команда значительно сильнее (разница в рейтинге >200), этот фактор доминирует.
2. Драфт — матчапы и синергии (20%): Контрпики, комбо героев, масштабирование по стадиям игры. При равных командах это решающий фактор.
3. Лейн-матчапы (15%): Кто выигрывает каждый лайн (мид 1v1, сайд-лайны 2v2). Раннее преимущество конвертируется в победу в ~60% про-матчей.
4. Мета патча и турнира (10%): Сильны ли выбранные герои в текущем патче и на этом турнире? Мета-герои vs нишевые пики.
5. Комфорт игроков на героях (10%): Сигнатурные герои vs непривычные пики. Про-игрок на сигнатуре играет на 5-10% сильнее.
6. Опыт команды на героях (10%): Как команда перформит на конкретных пиках. Отрепетированные стратегии vs импровизация.
7. История личных встреч (10%): Психологический фактор, стилистические матчапы. На малой выборке (<5 матчей) вес снижается.

=== МЕТОДОЛОГИЯ ===
Базовая вероятность = 50/50. Каждый фактор сдвигает вероятность пропорционально своему весу.
Для каждого фактора определи: сторону с преимуществом и степень (незначительное/умеренное/значительное).

=== ФОРМАТ ОТВЕТА (JSON) ===
Ответь валидным JSON-объектом с полями:
- "factors": массив из 7 объектов с полями:
  - "name": название фактора
  - "weight": вес в процентах (число)
  - "advantage": "Radiant", "Dire" или "Equal"
  - "degree": "незначительное", "умеренное" или "значительное"
  - "reasoning": обоснование (1-2 предложения с конкретными цифрами из данных)
- "winner": название команды-фаворита
- "radiant_win_prob": вероятность победы Radiant (число 0-100, допустимы десятичные)
- "dire_win_prob": вероятность победы Dire (число 0-100, допустимы десятичные)
- "confidence": "низкая", "средняя" или "высокая"
- "key_factors": массив из 3-5 строк — ключевые факторы с конкретными цифрами
- "analysis": детальный анализ (2-3 абзаца, Markdown-разметка, ссылки на конкретные данные)
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
