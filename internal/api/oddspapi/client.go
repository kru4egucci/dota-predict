package oddspapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"dota-predict/internal/models"
)

const (
	baseURL     = "https://api.oddspapi.io/v4"
	dotaSportID = 16
	// Market IDs for moneyline by map.
	marketSeries = "161" // series winner
	marketMap1   = "1647"
	marketMap2   = "1649"
	marketMap3   = "1651"
	// Outcome IDs: first outcome = participant1, second = participant2.
	outcomeHome = "161"
	outcomeAway = "162"
	// Preferred bookmaker (Pinnacle has per-map odds).
	preferredBookmaker = "pinnacle"
)

// mapMarkets maps game number (1-based) to OddsPapi market ID.
var mapMarkets = map[int]string{
	1: marketMap1,
	2: marketMap2,
	3: marketMap3,
}

// Map-specific outcome IDs (home/away per map).
var mapOutcomes = map[string][2]string{
	marketMap1: {"1647", "1648"},
	marketMap2: {"1649", "1650"},
	marketMap3: {"1651", "1652"},
}

// Client is an OddsPapi API client with fixture/odds caching.
type Client struct {
	httpClient *http.Client
	apiKey     string

	mu    sync.Mutex
	cache map[string]*cachedFixture // key: "team1 vs team2" normalized
}

type cachedFixture struct {
	fixture   *models.OddsFixture
	fetchedAt time.Time
}

const fixtureCacheTTL = 10 * time.Minute

// NewClient creates a new OddsPapi client.
func NewClient(apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		apiKey:     apiKey,
		cache:      make(map[string]*cachedFixture),
	}
}

// FindMatchOdds searches for a Dota 2 fixture matching the given team names
// and returns the best available odds.
// gameNumber is 1-based (1 = map 1, 2 = map 2, etc). If 0, returns series odds.
func (c *Client) FindMatchOdds(ctx context.Context, team1, team2 string, gameNumber int) (*models.MatchOdds, error) {
	log := slog.With("team1", team1, "team2", team2, "game_number", gameNumber)

	fixture, oddsResp, err := c.getFixtureAndOdds(ctx, team1, team2)
	if err != nil {
		log.Warn("oddspapi: не удалось получить данные", "error", err)
		return nil, err
	}

	odds, err := c.extractOdds(oddsResp, fixture, gameNumber)
	if err != nil {
		log.Warn("oddspapi: не удалось извлечь коэффициенты", "fixture_id", fixture.FixtureID, "error", err)
		return nil, err
	}

	log.Info("oddspapi: коэффициенты получены",
		"fixture_id", fixture.FixtureID,
		"bookmaker", odds.Bookmaker,
		"map", odds.MapNumber,
		"team1_name", odds.Team1Name, "team1_odds", odds.Team1Odds,
		"team2_name", odds.Team2Name, "team2_odds", odds.Team2Odds,
	)

	return odds, nil
}

// getFixtureAndOdds returns fixture (cached) and always-fresh odds.
func (c *Client) getFixtureAndOdds(ctx context.Context, team1, team2 string) (*models.OddsFixture, *models.OddsResponse, error) {
	fixture, err := c.getCachedFixture(ctx, team1, team2)
	if err != nil {
		return nil, nil, err
	}

	// Always fetch fresh odds for live data.
	oddsResp, err := c.fetchOdds(ctx, fixture.FixtureID)
	if err != nil {
		return nil, nil, err
	}

	return fixture, oddsResp, nil
}

// getCachedFixture returns a cached fixture or fetches a fresh one.
func (c *Client) getCachedFixture(ctx context.Context, team1, team2 string) (*models.OddsFixture, error) {
	cacheKey := normalizeCacheKey(team1, team2)

	c.mu.Lock()
	cached, ok := c.cache[cacheKey]
	if ok && time.Since(cached.fetchedAt) < fixtureCacheTTL {
		c.mu.Unlock()
		slog.Debug("oddspapi: фикстура из кеша", "fixture_id", cached.fixture.FixtureID)
		return cached.fixture, nil
	}
	c.mu.Unlock()

	fixture, err := c.findFixture(ctx, team1, team2)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[cacheKey] = &cachedFixture{
		fixture:   fixture,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return fixture, nil
}

// findFixture searches OddsPapi fixtures for a match between the two teams.
func (c *Client) findFixture(ctx context.Context, team1, team2 string) (*models.OddsFixture, error) {
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")
	to := now.Add(48 * time.Hour).Format("2006-01-02T15:04:05Z")
	url := fmt.Sprintf("%s/fixtures?sportId=%d&hasOdds=true&from=%s&to=%s&apiKey=%s",
		baseURL, dotaSportID, from, to, c.apiKey)

	var fixtures []models.OddsFixture
	if err := c.get(ctx, url, &fixtures); err != nil {
		return nil, fmt.Errorf("fetch fixtures: %w", err)
	}

	slog.Debug("oddspapi: фикстуры получены", "count", len(fixtures))

	t1 := strings.ToLower(team1)
	t2 := strings.ToLower(team2)

	for i, f := range fixtures {
		p1 := strings.ToLower(f.Participant1Name)
		p2 := strings.ToLower(f.Participant2Name)

		if (fuzzyMatch(p1, t1) && fuzzyMatch(p2, t2)) ||
			(fuzzyMatch(p1, t2) && fuzzyMatch(p2, t1)) {
			return &fixtures[i], nil
		}
	}

	return nil, fmt.Errorf("fixture not found for %s vs %s", team1, team2)
}

// fetchOdds fetches the raw odds response for a fixture.
func (c *Client) fetchOdds(ctx context.Context, fixtureID string) (*models.OddsResponse, error) {
	url := fmt.Sprintf("%s/odds?fixtureId=%s&apiKey=%s", baseURL, fixtureID, c.apiKey)

	var resp models.OddsResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch odds for fixture %s: %w", fixtureID, err)
	}

	return &resp, nil
}

// extractOdds pulls match winner odds for a specific map (or series).
func (c *Client) extractOdds(resp *models.OddsResponse, fixture *models.OddsFixture, gameNumber int) (*models.MatchOdds, error) {
	// Determine which market to look for.
	targetMarket := marketSeries
	homeOutcome := outcomeHome
	awayOutcome := outcomeAway
	if gameNumber > 0 {
		if m, ok := mapMarkets[gameNumber]; ok {
			targetMarket = m
		}
		if outs, ok := mapOutcomes[targetMarket]; ok {
			homeOutcome = outs[0]
			awayOutcome = outs[1]
		}
	}

	// Try preferred bookmaker first (Pinnacle has per-map odds), then others.
	bookmakers := []string{preferredBookmaker}
	for bm := range resp.BookmakerOdds {
		if bm != preferredBookmaker {
			bookmakers = append(bookmakers, bm)
		}
	}

	for _, bm := range bookmakers {
		entry, ok := resp.BookmakerOdds[bm]
		if !ok || !entry.BookmakerIsActive {
			continue
		}

		market, ok := entry.Markets[targetMarket]
		if !ok {
			continue
		}

		homePrice, homeOk := getPrice(market, homeOutcome)
		awayPrice, awayOk := getPrice(market, awayOutcome)
		if !homeOk || !awayOk {
			continue
		}

		if homePrice <= 1 || awayPrice <= 1 {
			continue
		}

		return &models.MatchOdds{
			Bookmaker: bm,
			Team1Name: fixture.Participant1Name,
			Team1Odds: homePrice,
			Team2Name: fixture.Participant2Name,
			Team2Odds: awayPrice,
			MapNumber: gameNumber,
		}, nil
	}

	// If per-map odds not found and we were looking for a map, fall back to series.
	if gameNumber > 0 {
		slog.Debug("oddspapi: коэффициенты по карте не найдены, используем серию",
			"game_number", gameNumber, "fixture_id", fixture.FixtureID)
		return c.extractOdds(resp, fixture, 0)
	}

	return nil, fmt.Errorf("no match winner odds found for fixture %s (market %s)", fixture.FixtureID, targetMarket)
}

// getPrice extracts the price from a market outcome.
func getPrice(market models.OddsMarket, outcomeID string) (float64, bool) {
	outcome, ok := market.Outcomes[outcomeID]
	if !ok {
		return 0, false
	}
	// Player "0" is the main outcome line.
	player, ok := outcome.Players["0"]
	if !ok {
		return 0, false
	}
	return player.Price, true
}

func (c *Client) get(ctx context.Context, url string, result interface{}) error {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
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

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if attempt < maxRetries {
				wait := 3 * time.Second
				slog.Warn("oddspapi: rate limited, повтор",
					"attempt", attempt+1,
					"wait", wait.String(),
				)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return fmt.Errorf("OddsPapi API rate limited after %d retries", maxRetries)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
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

		err = json.NewDecoder(resp.Body).Decode(result)
		resp.Body.Close()
		return err
	}
	return fmt.Errorf("OddsPapi API: unexpected retry loop exit")
}

// normalizeCacheKey creates a stable cache key from two team names (order-independent).
func normalizeCacheKey(team1, team2 string) string {
	t1 := strings.ToLower(strings.TrimSpace(team1))
	t2 := strings.ToLower(strings.TrimSpace(team2))
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	return t1 + " vs " + t2
}

// fuzzyMatch checks if two team name strings are similar enough.
func fuzzyMatch(a, b string) bool {
	if a == b {
		return true
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
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
