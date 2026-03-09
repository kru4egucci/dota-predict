package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"dota-predict/internal/models"
)

// playerRole holds the inferred position for a player in the current match.
type playerRole struct {
	accountID int
	heroID    int
	position  int // 1=carry, 2=mid, 3=offlane, 4=soft support, 5=hard support
}

// inferTeamPositions determines positions 1-5 for a team's players.
//
// Algorithm:
//  1. For each player, find their dominant lane_role (>40% of recent games).
//  2. Assign core positions (1, 2, 3) to players with clear lane_role signal.
//     Mid is assigned first (most distinctive), then carry, then offlane.
//     Conflicts (two players claiming same role) are left for GPM resolution.
//  3. Remaining players (supports + any unresolved cores) are sorted by
//     average GPM descending and assigned the remaining positions in order.
func inferTeamPositions(teamPlayers []models.MatchPlayer, recentData map[int][]models.PlayerRecentMatch) []playerRole {
	type info struct {
		accountID   int
		heroID      int
		primaryRole int     // dominant lane_role (1-3), 0 if unclear
		roleShare   float64 // fraction of games in primaryRole
		avgGPM      float64
	}

	infos := make([]info, 0, len(teamPlayers))
	for _, p := range teamPlayers {
		inf := info{accountID: p.AccountID, heroID: p.HeroID}

		recent := recentData[p.AccountID]
		if len(recent) > 0 {
			var roleCounts [5]int // index 1-4 used
			gpmSum := 0
			roleTotal := 0
			for _, m := range recent {
				gpmSum += m.GoldPerMin
				if m.LaneRole >= 1 && m.LaneRole <= 4 {
					roleCounts[m.LaneRole]++
					roleTotal++
				}
			}
			inf.avgGPM = float64(gpmSum) / float64(len(recent))

			// Find dominant core role (1-3 only; 4=jungle is not a core position).
			bestRole, bestCount := 0, 0
			for r := 1; r <= 3; r++ {
				if roleCounts[r] > bestCount {
					bestRole = r
					bestCount = roleCounts[r]
				}
			}
			if roleTotal > 0 && float64(bestCount)/float64(roleTotal) > 0.4 {
				inf.primaryRole = bestRole
				inf.roleShare = float64(bestCount) / float64(roleTotal)
			}
		}

		infos = append(infos, inf)
	}

	// Step 1: Assign core positions from lane_role data.
	// Process in priority order: mid (most distinctive), then carry, then offlane.
	assigned := make(map[int]bool) // infos index -> used
	posUsed := make(map[int]bool)  // position -> used
	posMap := make(map[int]int)    // accountID -> position

	for _, targetRole := range []int{2, 1, 3} {
		var candidates []int
		for i, inf := range infos {
			if !assigned[i] && inf.primaryRole == targetRole {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 1 {
			idx := candidates[0]
			assigned[idx] = true
			posUsed[targetRole] = true
			posMap[infos[idx].accountID] = targetRole
		}
		// Multiple candidates for the same role → leave for GPM resolution.
	}

	// Step 2: Remaining players sorted by GPM descending.
	var remaining []int
	for i := range infos {
		if !assigned[i] {
			remaining = append(remaining, i)
		}
	}
	sort.Slice(remaining, func(a, b int) bool {
		return infos[remaining[a]].avgGPM > infos[remaining[b]].avgGPM
	})

	// Available positions in priority order (higher GPM → lower position number).
	var availablePos []int
	for _, p := range []int{1, 2, 3, 4, 5} {
		if !posUsed[p] {
			availablePos = append(availablePos, p)
		}
	}
	for i, idx := range remaining {
		if i < len(availablePos) {
			posMap[infos[idx].accountID] = availablePos[i]
		}
	}

	// Build sorted result.
	result := make([]playerRole, 0, len(teamPlayers))
	for _, p := range teamPlayers {
		pos := posMap[p.AccountID]
		if pos == 0 {
			pos = 5
		}
		result = append(result, playerRole{
			accountID: p.AccountID,
			heroID:    p.HeroID,
			position:  pos,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].position < result[j].position
	})
	return result
}

// writeLaneMatchupSection adds lane matchup analysis to the prompt.
// Lanes in Dota 2:
//
//	Mid:        Radiant pos 2  vs  Dire pos 2
//	Bot lane:   Radiant pos 1+5 (safe) vs  Dire pos 3+4 (off)
//	Top lane:   Radiant pos 3+4 (off)  vs  Dire pos 1+5 (safe)
func writeLaneMatchupSection(sb *strings.Builder, data *models.CollectedData) {
	var radiantPlayers, direPlayers []models.MatchPlayer
	for _, p := range data.Match.Players {
		if p.IsRadiant {
			radiantPlayers = append(radiantPlayers, p)
		} else {
			direPlayers = append(direPlayers, p)
		}
	}

	if len(radiantPlayers) < 5 || len(direPlayers) < 5 {
		return
	}

	rRoles := inferTeamPositions(radiantPlayers, data.PlayerRecent)
	dRoles := inferTeamPositions(direPlayers, data.PlayerRecent)

	if len(rRoles) < 5 || len(dRoles) < 5 {
		return
	}

	rByPos := make(map[int]playerRole)
	dByPos := make(map[int]playerRole)
	for _, r := range rRoles {
		rByPos[r.position] = r
	}
	for _, d := range dRoles {
		dByPos[d.position] = d
	}

	sb.WriteString("## 6. ЛЕЙН-МАТЧАПЫ (Начальная расстановка)\n")
	sb.WriteString("Позиции определены на основе истории матчей игроков.\n\n")

	// Mid: pos 2 vs pos 2.
	writeLane(sb, "Мид (1v1)", "Radiant", "Dire", data,
		[]playerRole{rByPos[2]},
		[]playerRole{dByPos[2]})

	// Bot lane: Radiant safe (pos 1+5) vs Dire off (pos 3+4).
	writeLane(sb, "Нижний лайн (Radiant safe / Dire off)", "Radiant", "Dire", data,
		[]playerRole{rByPos[1], rByPos[5]},
		[]playerRole{dByPos[3], dByPos[4]})

	// Top lane: Radiant off (pos 3+4) vs Dire safe (pos 1+5).
	writeLane(sb, "Верхний лайн (Radiant off / Dire safe)", "Radiant", "Dire", data,
		[]playerRole{rByPos[3], rByPos[4]},
		[]playerRole{dByPos[1], dByPos[5]})

	sb.WriteString("\n")
}

// posLabel returns a short label for a Dota position number.
func posLabel(pos int) string {
	switch pos {
	case 1:
		return "carry"
	case 2:
		return "mid"
	case 3:
		return "offlane"
	case 4:
		return "soft sup"
	case 5:
		return "hard sup"
	default:
		return fmt.Sprintf("pos%d", pos)
	}
}

func writeLane(sb *strings.Builder, laneName, sideA, sideB string,
	data *models.CollectedData, allies, enemies []playerRole) {

	sb.WriteString(fmt.Sprintf("  %s:\n", laneName))

	// List heroes on each side with position labels.
	var aDescs, eDescs []string
	for _, a := range allies {
		aDescs = append(aDescs, fmt.Sprintf("%s (%s)", heroName(data.HeroNames, a.heroID), posLabel(a.position)))
	}
	for _, e := range enemies {
		eDescs = append(eDescs, fmt.Sprintf("%s (%s)", heroName(data.HeroNames, e.heroID), posLabel(e.position)))
	}
	sb.WriteString(fmt.Sprintf("    %s: %s\n", sideA, strings.Join(aDescs, " + ")))
	sb.WriteString(fmt.Sprintf("    %s: %s\n", sideB, strings.Join(eDescs, " + ")))

	// Show relevant hero matchups within this lane.
	for _, a := range allies {
		matchups := data.HeroMatchups[a.heroID]
		aName := heroName(data.HeroNames, a.heroID)
		for _, e := range enemies {
			eName := heroName(data.HeroNames, e.heroID)
			for _, mu := range matchups {
				if mu.HeroID == e.heroID && mu.GamesPlayed > 0 {
					wr := safePercent(mu.Wins, mu.GamesPlayed)
					sb.WriteString(fmt.Sprintf("    %s vs %s: %.1f%% (%d игр)\n",
						aName, eName, wr, mu.GamesPlayed))
				}
			}
		}
	}
}
