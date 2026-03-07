package opendota

import (
	"context"
	"fmt"

	"dota-predict/internal/models"
)

// GetHeroes returns the list of all heroes with names and roles.
func (c *Client) GetHeroes(ctx context.Context) ([]models.Hero, error) {
	var heroes []models.Hero
	if err := c.get(ctx, "/heroes", &heroes); err != nil {
		return nil, fmt.Errorf("get heroes: %w", err)
	}
	return heroes, nil
}

// GetHeroStats returns aggregated hero statistics across all brackets.
func (c *Client) GetHeroStats(ctx context.Context) ([]models.HeroStats, error) {
	var stats []models.HeroStats
	if err := c.get(ctx, "/heroStats", &stats); err != nil {
		return nil, fmt.Errorf("get hero stats: %w", err)
	}
	return stats, nil
}

// GetHeroMatchups returns how a hero performs against every other hero.
func (c *Client) GetHeroMatchups(ctx context.Context, heroID int) ([]models.HeroMatchup, error) {
	var matchups []models.HeroMatchup
	if err := c.get(ctx, fmt.Sprintf("/heroes/%d/matchups", heroID), &matchups); err != nil {
		return nil, fmt.Errorf("get hero %d matchups: %w", heroID, err)
	}
	return matchups, nil
}
