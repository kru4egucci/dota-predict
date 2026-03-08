package oddspapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dota-predict/internal/models"
)

const (
	baseURL    = "https://api.oddspapi.io/v4"
	dotaSportID = 16
	// Market ID for Match Winner. Adjust if OddsPapi uses a different ID.
	matchWinnerMarketID = "171"
	// Preferred bookmaker (Pinnacle is considered a sharp bookmaker).
	preferredBookmaker = "pinnacle"
)

// Client is an OddsPapi API client.
type Client struct {
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a new OddsPapi client.
func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
	}
}

// FindMatchOdds searches for a Dota 2 fixture matching the given team names
// and returns the best available odds.
func (c *Client) FindMatchOdds(ctx context.Context, team1, team2 string) (*models.MatchOdds, error) {
	log := slog.With("team1", team1, "team2", team2)

	log.Debug("oddspapi: поиск фикстуры")
	fixture, err := c.findFixture(ctx, team1, team2)
	if err != nil {
		log.Warn("oddspapi: фикстура не найдена", "error", err)
		return nil, err
	}
	log.Debug("oddspapi: фикстура найдена", "fixture_id", fixture.ID, "fixture_name", fixture.Name)

	odds, err := c.getOdds(ctx, fixture)
	if err != nil {
		log.Warn("oddspapi: не удалось получить коэффициенты", "fixture_id", fixture.ID, "error", err)
		return nil, err
	}

	log.Info("oddspapi: коэффициенты получены",
		"fixture_id", fixture.ID,
		"bookmaker", odds.Bookmaker,
		"team1_name", odds.Team1Name, "team1_odds", odds.Team1Odds,
		"team2_name", odds.Team2Name, "team2_odds", odds.Team2Odds,
	)

	return odds, nil
}

// findFixture searches OddsPapi fixtures for a match between the two teams.
func (c *Client) findFixture(ctx context.Context, team1, team2 string) (*models.OddsFixture, error) {
	url := fmt.Sprintf("%s/fixtures?sportId=%d&hasOdds=true&apiKey=%s",
		baseURL, dotaSportID, c.apiKey)

	var resp models.OddsFixturesResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch fixtures: %w", err)
	}

	slog.Debug("oddspapi: фикстуры получены", "count", len(resp.Data))

	t1 := strings.ToLower(team1)
	t2 := strings.ToLower(team2)

	for i, f := range resp.Data {
		name := strings.ToLower(f.Name)

		if matchTeams(name, t1, t2) {
			return &resp.Data[i], nil
		}

		// Also check participants.
		if len(f.Participants) >= 2 {
			p1 := strings.ToLower(f.Participants[0].Name)
			p2 := strings.ToLower(f.Participants[1].Name)
			if (fuzzyMatch(p1, t1) && fuzzyMatch(p2, t2)) ||
				(fuzzyMatch(p1, t2) && fuzzyMatch(p2, t1)) {
				return &resp.Data[i], nil
			}
		}
	}

	return nil, fmt.Errorf("fixture not found for %s vs %s", team1, team2)
}

// getOdds fetches odds for a specific fixture and extracts match winner odds.
func (c *Client) getOdds(ctx context.Context, fixture *models.OddsFixture) (*models.MatchOdds, error) {
	url := fmt.Sprintf("%s/odds?fixtureId=%s&apiKey=%s",
		baseURL, fixture.ID, c.apiKey)

	var resp models.OddsResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch odds for fixture %s: %w", fixture.ID, err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no odds data for fixture %s", fixture.ID)
	}

	return c.extractOdds(&resp.Data[0], fixture)
}

// extractOdds pulls match winner odds, preferring Pinnacle.
func (c *Client) extractOdds(data *models.OddsData, fixture *models.OddsFixture) (*models.MatchOdds, error) {
	// Try preferred bookmaker first, then fall back to any available.
	bookmakers := []string{preferredBookmaker}
	for bm := range data.BookmakerOdds {
		if bm != preferredBookmaker {
			bookmakers = append(bookmakers, bm)
		}
	}

	slog.Debug("oddspapi: доступные букмекеры", "bookmakers", bookmakers)

	for _, bm := range bookmakers {
		entry, ok := data.BookmakerOdds[bm]
		if !ok {
			continue
		}

		market, ok := entry.Markets[matchWinnerMarketID]
		if !ok {
			continue
		}

		outcomes := make([]models.OddsOutcome, 0, len(market.Outcomes))
		for _, o := range market.Outcomes {
			outcomes = append(outcomes, o)
		}

		if len(outcomes) < 2 {
			continue
		}

		result := &models.MatchOdds{
			Bookmaker: bm,
		}

		// Match outcomes to fixture participants by name or by position.
		if len(fixture.Participants) >= 2 {
			result.Team1Name = fixture.Participants[0].Name
			result.Team2Name = fixture.Participants[1].Name
		}

		// Assign odds: if outcomes have names, match them; otherwise use position.
		if outcomes[0].Name != "" && outcomes[1].Name != "" {
			result.Team1Name = outcomes[0].Name
			result.Team1Odds = outcomes[0].Price
			result.Team2Name = outcomes[1].Name
			result.Team2Odds = outcomes[1].Price
		} else {
			result.Team1Odds = outcomes[0].Price
			result.Team2Odds = outcomes[1].Price
		}

		return result, nil
	}

	return nil, fmt.Errorf("no match winner odds found for fixture %s", fixture.ID)
}

func (c *Client) get(ctx context.Context, url string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("oddspapi: ошибка HTTP запроса",
			"duration", elapsed.String(),
			"error", err,
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("oddspapi: неуспешный HTTP статус",
			"status", resp.StatusCode,
			"body", string(body),
			"duration", elapsed.String(),
		)
		return fmt.Errorf("OddsPapi API returned %d: %s", resp.StatusCode, string(body))
	}

	slog.Debug("oddspapi: запрос выполнен",
		"status", resp.StatusCode,
		"duration", elapsed.String(),
	)

	return json.NewDecoder(resp.Body).Decode(result)
}

// matchTeams checks if the fixture name contains both team names.
func matchTeams(fixtureName, team1, team2 string) bool {
	return (strings.Contains(fixtureName, team1) && strings.Contains(fixtureName, team2))
}

// fuzzyMatch checks if two team name strings are similar enough.
// Handles cases like "Team Spirit" vs "Spirit", "BetBoom Team" vs "BetBoom".
func fuzzyMatch(a, b string) bool {
	if a == b {
		return true
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	// Remove common prefixes like "team", "esports", "gaming".
	a = stripCommon(a)
	b = stripCommon(b)
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

func stripCommon(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"team ", "esports", "gaming"} {
		s = strings.ReplaceAll(s, prefix, "")
	}
	return strings.TrimSpace(s)
}
