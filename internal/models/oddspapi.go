package models

// OddsFixture represents a fixture from the OddsPapi API.
type OddsFixture struct {
	ID           string             `json:"id"`
	SportID      int                `json:"sportId"`
	Name         string             `json:"name"` // e.g. "Team Spirit - Tundra Esports"
	StartDate    string             `json:"startDate"`
	Participants []OddsParticipant  `json:"participants"`
}

// OddsParticipant represents a team in a fixture.
type OddsParticipant struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// OddsFixturesResponse is the top-level response from /v4/fixtures.
type OddsFixturesResponse struct {
	Data []OddsFixture `json:"data"`
}

// OddsData represents the odds response from /v4/odds.
type OddsData struct {
	FixtureID     string                    `json:"fixtureId"`
	BookmakerOdds map[string]BookmakerEntry `json:"bookmakerOdds"`
}

// BookmakerEntry represents odds from a single bookmaker.
type BookmakerEntry struct {
	Markets map[string]OddsMarket `json:"markets"`
}

// OddsMarket represents a betting market (e.g. match winner).
type OddsMarket struct {
	Outcomes map[string]OddsOutcome `json:"outcomes"`
}

// OddsOutcome represents a single outcome with its odds.
type OddsOutcome struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

// OddsResponse is the top-level response from /v4/odds.
type OddsResponse struct {
	Data []OddsData `json:"data"`
}

// MatchOdds holds the final extracted odds for a match.
type MatchOdds struct {
	Bookmaker    string
	Team1Name    string
	Team1Odds    float64
	Team2Name    string
	Team2Odds    float64
}
