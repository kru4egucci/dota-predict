package models

// OddsFixture represents a fixture from the OddsPapi API.
// The /v4/fixtures endpoint returns []OddsFixture (a JSON array).
type OddsFixture struct {
	FixtureID        string `json:"fixtureId"`
	Participant1ID   int    `json:"participant1Id"`
	Participant2ID   int    `json:"participant2Id"`
	SportID          int    `json:"sportId"`
	Participant1Name string `json:"participant1Name"`
	Participant2Name string `json:"participant2Name"`
	TournamentName   string `json:"tournamentName"`
	StartTime        string `json:"startTime"`
	HasOdds          bool   `json:"hasOdds"`
}

// OddsResponse represents the /v4/odds response (a single JSON object).
type OddsResponse struct {
	FixtureID     string                    `json:"fixtureId"`
	BookmakerOdds map[string]BookmakerEntry `json:"bookmakerOdds"`
}

// BookmakerEntry represents odds from a single bookmaker.
type BookmakerEntry struct {
	BookmakerIsActive bool                      `json:"bookmakerIsActive"`
	Markets           map[string]OddsMarket     `json:"markets"`
}

// OddsMarket represents a betting market (e.g. match winner moneyline).
type OddsMarket struct {
	Outcomes         map[string]OddsOutcomeWrap `json:"outcomes"`
	BookmakerMarketID string                    `json:"bookmakerMarketId"`
}

// OddsOutcomeWrap wraps the players map for an outcome.
type OddsOutcomeWrap struct {
	Players map[string]OddsPlayer `json:"players"`
}

// OddsPlayer holds the actual odds data for a player/outcome.
type OddsPlayer struct {
	Active bool    `json:"active"`
	Price  float64 `json:"price"`
}

// MatchOdds holds the final extracted odds for a match.
type MatchOdds struct {
	Bookmaker string
	Team1Name string
	Team1Odds float64
	Team2Name string
	Team2Odds float64
	MapNumber int // 0 = series, 1/2/3 = specific map
}
