package opendota

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"time"

	"dota-predict/internal/models"
)

// explorerResult is the raw response from the GET /explorer endpoint.
type explorerResult struct {
	Command  string                   `json:"command"`
	RowCount int                      `json:"rowCount"`
	Rows     []map[string]interface{} `json:"rows"`
	Err      *string                  `json:"err"`
}

// explorer executes a read-only SQL query via the OpenDota Explorer endpoint.
func (c *Client) explorer(ctx context.Context, sql string) (*explorerResult, error) {
	var result explorerResult
	path := "/explorer?sql=" + url.QueryEscape(sql)
	if err := c.get(ctx, path, &result); err != nil {
		return nil, err
	}
	if result.Err != nil {
		return nil, fmt.Errorf("explorer SQL error: %s", *result.Err)
	}
	return &result, nil
}

// GetCurrentPatch returns the most recent patch version string (e.g. "7.40").
// Cached for 24 hours.
func (c *Client) GetCurrentPatch(ctx context.Context) (string, error) {
	const key = "currentPatch"
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.(string), nil
	}

	result, err := c.explorer(ctx,
		"SELECT DISTINCT patch FROM match_patch ORDER BY patch DESC LIMIT 1")
	if err != nil {
		return "", fmt.Errorf("get current patch: %w", err)
	}
	if len(result.Rows) == 0 {
		return "", fmt.Errorf("no patch data found")
	}

	patch := fmt.Sprintf("%v", result.Rows[0]["patch"])
	c.cache.set(key, patch, 24*time.Hour)
	slog.Debug("opendota: текущий патч определён", "patch", patch)
	return patch, nil
}

// GetHeroStatsByPatch returns hero win rates across all matches on a given patch.
// Cached for 6 hours.
func (c *Client) GetHeroStatsByPatch(ctx context.Context, patch string) ([]models.HeroPatchStats, error) {
	key := "heroPatchStats:" + patch
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.HeroPatchStats), nil
	}

	sql := fmt.Sprintf(
		`SELECT pm.hero_id,
			COUNT(*) as games,
			SUM(CASE WHEN (pm.player_slot < 128) = m.radiant_win THEN 1 ELSE 0 END) as wins
		FROM player_matches pm
		JOIN matches m USING(match_id)
		JOIN match_patch mp USING(match_id)
		WHERE mp.patch = '%s'
		GROUP BY pm.hero_id`, patch)

	result, err := c.explorer(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("get hero stats by patch %s: %w", patch, err)
	}

	stats := make([]models.HeroPatchStats, 0, len(result.Rows))
	for _, row := range result.Rows {
		stats = append(stats, models.HeroPatchStats{
			HeroID: toInt(row["hero_id"]),
			Games:  toInt(row["games"]),
			Wins:   toInt(row["wins"]),
		})
	}

	c.cache.set(key, stats, 6*time.Hour)
	slog.Debug("opendota: статистика героев по патчу загружена",
		"patch", patch, "heroes", len(stats))
	return stats, nil
}

// GetHeroLeagueStats returns hero pick/ban/win stats for a specific league.
// Executes two Explorer queries (pick/ban counts + win counts) and merges them.
// Cached for 1 hour.
func (c *Client) GetHeroLeagueStats(ctx context.Context, leagueID int) ([]models.HeroLeagueStats, error) {
	key := fmt.Sprintf("heroLeagueStats:%d", leagueID)
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.HeroLeagueStats), nil
	}

	// Query 1: Pick/ban counts from the draft table.
	pickBanSQL := fmt.Sprintf(
		`SELECT hero_id,
			SUM(CASE WHEN is_pick THEN 1 ELSE 0 END) as picks,
			SUM(CASE WHEN NOT is_pick THEN 1 ELSE 0 END) as bans
		FROM picks_bans
		JOIN matches USING(match_id)
		WHERE matches.leagueid = %d
		GROUP BY hero_id`, leagueID)

	pbResult, err := c.explorer(ctx, pickBanSQL)
	if err != nil {
		return nil, fmt.Errorf("get league %d pick/ban stats: %w", leagueID, err)
	}

	// Query 2: Win counts from player match data.
	winSQL := fmt.Sprintf(
		`SELECT pm.hero_id,
			SUM(CASE WHEN (pm.player_slot < 128) = m.radiant_win THEN 1 ELSE 0 END) as wins
		FROM player_matches pm
		JOIN matches m USING(match_id)
		WHERE m.leagueid = %d
		GROUP BY pm.hero_id`, leagueID)

	winResult, err := c.explorer(ctx, winSQL)
	if err != nil {
		return nil, fmt.Errorf("get league %d win stats: %w", leagueID, err)
	}

	// Merge pick/ban data with win data by hero_id.
	winMap := make(map[int]int, len(winResult.Rows))
	for _, row := range winResult.Rows {
		winMap[toInt(row["hero_id"])] = toInt(row["wins"])
	}

	stats := make([]models.HeroLeagueStats, 0, len(pbResult.Rows))
	for _, row := range pbResult.Rows {
		heroID := toInt(row["hero_id"])
		stats = append(stats, models.HeroLeagueStats{
			HeroID: heroID,
			Picks:  toInt(row["picks"]),
			Bans:   toInt(row["bans"]),
			Wins:   winMap[heroID],
		})
	}

	c.cache.set(key, stats, 1*time.Hour)
	slog.Debug("opendota: статистика героев турнира загружена",
		"league_id", leagueID, "heroes", len(stats))
	return stats, nil
}

// toInt converts an interface{} (typically float64 from JSON) to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(math.Round(n))
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
