package tier1

// Teams is a hardcoded map of tier-1 Dota 2 team IDs to their names.
// Keep this list up to date — add/remove teams as the competitive scene evolves.
var Teams = map[int]string{
	7119388: "Team Spirit",
	8599101: "Gaimin Gladiators",
	8291895: "Tundra Esports",
	8255888: "BetBoom Team",
	9247354: "Team Falcons",
	2163:    "Team Liquid",
	1838315: "Team Secret",
	36:      "Natus Vincere",
	2586976: "OG",
	1883502: "Virtus.pro",
	15:      "PSG.LGD",
	8261500: "Xtreme Gaming",
	9467224: "Aurora Gaming",
	9823272: "Team Yandex",
	9572001: "Parivision",
}

// IsTier1 returns true if the given team ID is in the tier-1 list.
func IsTier1(teamID int) bool {
	_, ok := Teams[teamID]
	return ok
}

// HasTier1Team returns true if at least one of the two team IDs is tier-1.
func HasTier1Team(radiantTeamID, direTeamID int) bool {
	return IsTier1(radiantTeamID) || IsTier1(direTeamID)
}
