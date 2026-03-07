package opendota

import (
	"context"
	"fmt"

	"dota-predict/internal/models"
)

// GetPlayerHeroes fetches a player's hero statistics.
func (c *Client) GetPlayerHeroes(ctx context.Context, accountID int) ([]models.PlayerHeroStat, error) {
	var heroes []models.PlayerHeroStat
	if err := c.get(ctx, fmt.Sprintf("/players/%d/heroes", accountID), &heroes); err != nil {
		return nil, fmt.Errorf("get player %d heroes: %w", accountID, err)
	}
	return heroes, nil
}

// GetPlayerRecentMatches fetches a player's recent matches.
func (c *Client) GetPlayerRecentMatches(ctx context.Context, accountID int) ([]models.PlayerRecentMatch, error) {
	var matches []models.PlayerRecentMatch
	if err := c.get(ctx, fmt.Sprintf("/players/%d/recentMatches", accountID), &matches); err != nil {
		return nil, fmt.Errorf("get player %d recent matches: %w", accountID, err)
	}
	return matches, nil
}
