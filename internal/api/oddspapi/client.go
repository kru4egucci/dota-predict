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

	oddsMu    sync.RWMutex
	oddsCache map[string]*models.MatchOdds // key: "fixtureId:mapNumber"
}

type cachedFixture struct {
	fixture   *models.OddsFixture
	fetchedAt time.Time
}

const fixtureCacheTTL = 10 * time.Minute
const oddsRefreshInterval = 1 * time.Hour

// NewClient creates a new OddsPapi client.
func NewClient(apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		apiKey:     apiKey,
		cache:      make(map[string]*cachedFixture),
		oddsCache:  make(map[string]*models.MatchOdds),
	}
}

// FindMatchOdds searches for a Dota 2 fixture matching the given team names
// and returns the best available odds.
// gameNumber is 1-based (1 = map 1, 2 = map 2, etc). If 0, returns series odds.
func (c *Client) FindMatchOdds(ctx context.Context, team1, team2 string, gameNumber int) (*models.MatchOdds, error) {
	log := slog.With("team1", team1, "team2", team2, "game_number", gameNumber)

	fixture, err := c.getCachedFixture(ctx, team1, team2)
	if err != nil {
		log.Warn("oddspapi: не удалось найти фикстуру", "error", err)
		return nil, err
	}

	// Check odds cache first.
	cacheKey := oddsCacheKey(fixture.FixtureID, gameNumber)
	c.oddsMu.RLock()
	cached, ok := c.oddsCache[cacheKey]
	c.oddsMu.RUnlock()

	if ok && cached != nil {
		log.Info("oddspapi: коэффициенты из кеша",
			"fixture_id", fixture.FixtureID,
			"bookmaker", cached.Bookmaker,
			"map", cached.MapNumber,
			"team1_name", cached.Team1Name, "team1_odds", cached.Team1Odds,
			"team2_name", cached.Team2Name, "team2_odds", cached.Team2Odds,
		)
		return cached, nil
	}

	// Cache miss — fetch live odds.
	log.Info("oddspapi: кеш пуст, запрос лайв коэффициентов", "fixture_id", fixture.FixtureID)
	odds, err := c.tryLiveOdds(ctx, fixture, gameNumber)
	if err != nil {
		slog.Debug("oddspapi: live odds недоступны, пробуем historical", "fixture_id", fixture.FixtureID, "error", err)
		odds, err = c.tryHistoricalOdds(ctx, fixture, gameNumber)
	}
	if err != nil {
		log.Warn("oddspapi: не удалось получить коэффициенты", "fixture_id", fixture.FixtureID, "error", err)
		return nil, err
	}

	// Save to cache.
	c.oddsMu.Lock()
	c.oddsCache[cacheKey] = odds
	c.oddsMu.Unlock()

	log.Info("oddspapi: коэффициенты получены",
		"fixture_id", fixture.FixtureID,
		"bookmaker", odds.Bookmaker,
		"map", odds.MapNumber,
		"team1_name", odds.Team1Name, "team1_odds", odds.Team1Odds,
		"team2_name", odds.Team2Name, "team2_odds", odds.Team2Odds,
	)

	return odds, nil
}

// tryLiveOdds fetches current odds from /v4/odds.
func (c *Client) tryLiveOdds(ctx context.Context, fixture *models.OddsFixture, gameNumber int) (*models.MatchOdds, error) {
	resp, err := c.fetchOddsResponse(ctx, fixture.FixtureID)
	if err != nil {
		return nil, err
	}
	return c.extractOdds(resp, fixture, gameNumber)
}

// tryHistoricalOdds fetches latest odds from /v4/historical-odds (uses newest entry as current price).
func (c *Client) tryHistoricalOdds(ctx context.Context, fixture *models.OddsFixture, gameNumber int) (*models.MatchOdds, error) {
	url := fmt.Sprintf("%s/historical-odds?fixtureId=%s&apiKey=%s", baseURL, fixture.FixtureID, c.apiKey)

	var resp models.HistoricalOddsResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch historical odds: %w", err)
	}

	if len(resp.Bookmakers) == 0 {
		return nil, fmt.Errorf("no bookmaker data in historical odds for fixture %s", fixture.FixtureID)
	}

	// Convert historical response to the standard OddsResponse format (take latest price).
	converted := c.convertHistoricalToOdds(&resp)
	return c.extractOdds(converted, fixture, gameNumber)
}

// convertHistoricalToOdds takes historical odds and builds a standard OddsResponse
// using the latest (first) entry for each outcome.
func (c *Client) convertHistoricalToOdds(hist *models.HistoricalOddsResponse) *models.OddsResponse {
	resp := &models.OddsResponse{
		FixtureID:     hist.FixtureID,
		BookmakerOdds: make(map[string]models.BookmakerEntry),
	}

	for bmSlug, bmData := range hist.Bookmakers {
		entry := models.BookmakerEntry{
			BookmakerIsActive: true,
			Markets:           make(map[string]models.OddsMarket),
		}

		for marketID, market := range bmData.Markets {
			oddsMarket := models.OddsMarket{
				Outcomes: make(map[string]models.OddsOutcomeWrap),
			}

			for outcomeID, outcome := range market.Outcomes {
				wrap := models.OddsOutcomeWrap{
					Players: make(map[string]models.OddsPlayer),
				}

				for playerID, entries := range outcome.Players {
					if len(entries) > 0 {
						// First entry is the latest (newest).
						wrap.Players[playerID] = models.OddsPlayer{
							Active: entries[0].Active,
							Price:  entries[0].Price,
						}
					}
				}

				oddsMarket.Outcomes[outcomeID] = wrap
			}

			entry.Markets[marketID] = oddsMarket
		}

		resp.BookmakerOdds[bmSlug] = entry
	}

	return resp
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
	url := fmt.Sprintf("%s/fixtures?sportId=%d&from=%s&to=%s&apiKey=%s",
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

	// Try all available bookmakers.
	for bm, entry := range resp.BookmakerOdds {
		if !entry.BookmakerIsActive {
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

// oddsCacheKey builds the key for the odds cache.
func oddsCacheKey(fixtureID string, gameNumber int) string {
	return fmt.Sprintf("%s:%d", fixtureID, gameNumber)
}

// StartPeriodicRefresh starts a background goroutine that refreshes odds for all
// Dota 2 fixtures every hour. Cached odds are only overwritten when new data is found.
func (c *Client) StartPeriodicRefresh(ctx context.Context) {
	slog.Info("oddspapi: запуск фонового обновления коэффициентов", "interval", oddsRefreshInterval.String())

	// Run immediately on start.
	c.refreshAllOdds(ctx)

	go func() {
		ticker := time.NewTicker(oddsRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("oddspapi: фоновое обновление остановлено")
				return
			case <-ticker.C:
				c.refreshAllOdds(ctx)
			}
		}
	}()
}

// refreshAllOdds fetches all fixtures and caches odds for maps 1-3.
func (c *Client) refreshAllOdds(ctx context.Context) {
	slog.Info("oddspapi: обновление коэффициентов для всех фикстур")

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")
	to := now.Add(48 * time.Hour).Format("2006-01-02T15:04:05Z")
	url := fmt.Sprintf("%s/fixtures?sportId=%d&from=%s&to=%s&apiKey=%s",
		baseURL, dotaSportID, from, to, c.apiKey)

	var fixtures []models.OddsFixture
	if err := c.get(ctx, url, &fixtures); err != nil {
		slog.Error("oddspapi: ошибка получения фикстур для обновления", "error", err)
		return
	}

	updated := 0
	for i := range fixtures {
		f := &fixtures[i]
		if !f.HasOdds {
			continue
		}

		// Try to fetch odds for this fixture.
		resp, err := c.fetchOddsResponse(ctx, f.FixtureID)
		if err != nil {
			slog.Debug("oddspapi: не удалось получить коэффициенты фикстуры",
				"fixture_id", f.FixtureID,
				"teams", f.Participant1Name+" vs "+f.Participant2Name,
				"error", err,
			)
			continue
		}

		// Try to extract odds for maps 1-3.
		for mapNum := 1; mapNum <= 3; mapNum++ {
			odds, err := c.extractOdds(resp, f, mapNum)
			if err != nil {
				continue
			}

			key := oddsCacheKey(f.FixtureID, mapNum)
			c.oddsMu.Lock()
			c.oddsCache[key] = odds
			c.oddsMu.Unlock()
			updated++
		}

		// Also cache fixture for team name lookups.
		cacheKey := normalizeCacheKey(f.Participant1Name, f.Participant2Name)
		c.mu.Lock()
		c.cache[cacheKey] = &cachedFixture{
			fixture:   f,
			fetchedAt: time.Now(),
		}
		c.mu.Unlock()
	}

	slog.Info("oddspapi: обновление завершено",
		"fixtures_total", len(fixtures),
		"odds_cached", updated,
	)
}

// fetchOddsResponse fetches the raw odds response for a fixture.
func (c *Client) fetchOddsResponse(ctx context.Context, fixtureID string) (*models.OddsResponse, error) {
	url := fmt.Sprintf("%s/odds?fixtureId=%s&apiKey=%s", baseURL, fixtureID, c.apiKey)

	var resp models.OddsResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch odds: %w", err)
	}

	if len(resp.BookmakerOdds) == 0 {
		return nil, fmt.Errorf("no bookmakerOdds in response for fixture %s", fixtureID)
	}

	return &resp, nil
}

func stripCommon(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"team ", "esports", "gaming"} {
		s = strings.ReplaceAll(s, prefix, "")
	}
	return strings.TrimSpace(s)
}
