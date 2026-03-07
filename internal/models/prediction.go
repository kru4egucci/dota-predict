package models

import "sync"

// CollectedData holds all data gathered for a match prediction.
type CollectedData struct {
	Match     Match
	HeroNames map[int]string // hero_id -> localized_name

	// Hero analysis.
	HeroStats    map[int]*HeroStats    // hero_id -> stats
	HeroMatchups map[int][]HeroMatchup // hero_id -> matchups vs enemies

	// Team analysis (nil if not a pro match).
	RadiantTeam        *Team
	DireTeam           *Team
	RadiantTeamMatches []TeamMatch
	DireTeamMatches    []TeamMatch
	RadiantTeamHeroes  []TeamHero
	DireTeamHeroes     []TeamHero
	HeadToHead         []TeamMatch

	// Player analysis (concurrent-safe via mutex).
	mu              sync.Mutex
	PlayerHeroStats map[int]*PlayerHeroStat     // account_id -> stat for picked hero
	PlayerRecent    map[int][]PlayerRecentMatch  // account_id -> recent matches
}

// SetPlayerHeroStat safely sets a player's hero stat.
func (cd *CollectedData) SetPlayerHeroStat(accountID int, stat *PlayerHeroStat) {
	cd.mu.Lock()
	cd.PlayerHeroStats[accountID] = stat
	cd.mu.Unlock()
}

// SetPlayerRecent safely sets a player's recent matches.
func (cd *CollectedData) SetPlayerRecent(accountID int, matches []PlayerRecentMatch) {
	cd.mu.Lock()
	cd.PlayerRecent[accountID] = matches
	cd.mu.Unlock()
}

// Prediction is the final result from the LLM.
type Prediction struct {
	RadiantTeamName string
	DireTeamName    string
	Analysis        string
	DraftAnalysis   string
	Betting         BettingInfo
}

// BettingInfo contains parsed probabilities and calculated betting odds.
type BettingInfo struct {
	// Main analysis.
	RadiantWinProb float64 // 0-100
	DireWinProb    float64 // 0-100
	Confidence     string  // Низкая/Средняя/Высокая

	// Draft-only analysis.
	DraftRadiantProb float64
	DraftDireProb    float64

	// Calculated odds.
	RadiantMinOdds      float64 // fair value (break-even)
	RadiantComfortOdds  float64 // with margin based on confidence
	DireMinOdds         float64
	DireComfortOdds     float64
}
