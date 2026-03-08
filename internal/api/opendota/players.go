package opendota

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dota-predict/internal/models"
)

// GetPlayerHeroes fetches a player's hero statistics.
// Cached for 1 hour.
func (c *Client) GetPlayerHeroes(ctx context.Context, accountID int) ([]models.PlayerHeroStat, error) {
	key := fmt.Sprintf("playerHeroes:%d", accountID)
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.PlayerHeroStat), nil
	}
	var heroes []models.PlayerHeroStat
	if err := c.get(ctx, fmt.Sprintf("/players/%d/heroes", accountID), &heroes); err != nil {
		return nil, fmt.Errorf("get player %d heroes: %w", accountID, err)
	}
	c.cache.set(key, heroes, 1*time.Hour)
	return heroes, nil
}

// GetPlayerRecentMatches fetches a player's recent matches.
// Not cached — needs to be up-to-date.
func (c *Client) GetPlayerRecentMatches(ctx context.Context, accountID int) ([]models.PlayerRecentMatch, error) {
	var matches []models.PlayerRecentMatch
	if err := c.get(ctx, fmt.Sprintf("/players/%d/recentMatches", accountID), &matches); err != nil {
		return nil, fmt.Errorf("get player %d recent matches: %w", accountID, err)
	}
	return matches, nil
}
