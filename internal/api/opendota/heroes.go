package opendota

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dota-predict/internal/models"
)

// GetHeroes returns the list of all heroes with names and roles.
// Cached for 24 hours (changes only with game patches).
func (c *Client) GetHeroes(ctx context.Context) ([]models.Hero, error) {
	const key = "heroes"
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.Hero), nil
	}
	var heroes []models.Hero
	if err := c.get(ctx, "/heroes", &heroes); err != nil {
		return nil, fmt.Errorf("get heroes: %w", err)
	}
	c.cache.set(key, heroes, 24*time.Hour)
	return heroes, nil
}

// GetHeroStats returns aggregated hero statistics across all brackets.
// Cached for 6 hours (aggregated stats, changes slowly).
func (c *Client) GetHeroStats(ctx context.Context) ([]models.HeroStats, error) {
	const key = "heroStats"
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.HeroStats), nil
	}
	var stats []models.HeroStats
	if err := c.get(ctx, "/heroStats", &stats); err != nil {
		return nil, fmt.Errorf("get hero stats: %w", err)
	}
	c.cache.set(key, stats, 6*time.Hour)
	return stats, nil
}

// GetHeroMatchups returns how a hero performs against every other hero.
// Cached for 6 hours (aggregated matchup data, changes slowly).
func (c *Client) GetHeroMatchups(ctx context.Context, heroID int) ([]models.HeroMatchup, error) {
	key := fmt.Sprintf("heroMatchups:%d", heroID)
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.HeroMatchup), nil
	}
	var matchups []models.HeroMatchup
	if err := c.get(ctx, fmt.Sprintf("/heroes/%d/matchups", heroID), &matchups); err != nil {
		return nil, fmt.Errorf("get hero %d matchups: %w", heroID, err)
	}
	c.cache.set(key, matchups, 6*time.Hour)
	return matchups, nil
}
