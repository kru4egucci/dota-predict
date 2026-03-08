package tier1

// Tier1Teams is a hardcoded map of tier-1 Dota 2 team IDs to their names.
// Keep this list up to date — add/remove teams as the competitive scene evolves.
var Tier1Teams = map[int]string{
	8597976: "Team Spirit",
	8599101: "Gaimin Gladiators",
	8291895: "Tundra Esports",
	8605863: "BetBoom Team",
	8944440: "Team Falcons",
	2163:    "Team Liquid",
	1838315: "Team Secret",
	36:      "Natus Vincere",
	2586976: "OG",
	1883502: "Virtus.pro",
	15:      "PSG.LGD",
	8261648: "Xtreme Gaming",
}

// IsTier1 returns true if the given team ID is in the tier-1 list.
func IsTier1(teamID int) bool {
	_, ok := Tier1Teams[teamID]
	return ok
}

// HasTier1Team returns true if at least one of the two team IDs is tier-1.
func HasTier1Team(radiantTeamID, direTeamID int) bool {
	return IsTier1(radiantTeamID) || IsTier1(direTeamID)
}
