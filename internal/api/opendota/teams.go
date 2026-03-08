package opendota

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dota-predict/internal/models"
)

// GetTeam fetches team info including overall win/loss record.
// Cached for 1 hour.
func (c *Client) GetTeam(ctx context.Context, teamID int) (*models.Team, error) {
	key := fmt.Sprintf("team:%d", teamID)
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.(*models.Team), nil
	}
	var team models.Team
	if err := c.get(ctx, fmt.Sprintf("/teams/%d", teamID), &team); err != nil {
		return nil, fmt.Errorf("get team %d: %w", teamID, err)
	}
	c.cache.set(key, &team, 1*time.Hour)
	return &team, nil
}

// GetTeamMatches fetches a team's match history.
// Not cached — needs to be up-to-date.
func (c *Client) GetTeamMatches(ctx context.Context, teamID int) ([]models.TeamMatch, error) {
	var matches []models.TeamMatch
	if err := c.get(ctx, fmt.Sprintf("/teams/%d/matches", teamID), &matches); err != nil {
		return nil, fmt.Errorf("get team %d matches: %w", teamID, err)
	}
	return matches, nil
}

// GetTeamHeroes fetches hero-specific win rates for a team.
// Cached for 1 hour.
func (c *Client) GetTeamHeroes(ctx context.Context, teamID int) ([]models.TeamHero, error) {
	key := fmt.Sprintf("teamHeroes:%d", teamID)
	if v, ok := c.cache.get(key); ok {
		slog.Debug("opendota: cache hit", "key", key)
		return v.([]models.TeamHero), nil
	}
	var heroes []models.TeamHero
	if err := c.get(ctx, fmt.Sprintf("/teams/%d/heroes", teamID), &heroes); err != nil {
		return nil, fmt.Errorf("get team %d heroes: %w", teamID, err)
	}
	c.cache.set(key, heroes, 1*time.Hour)
	return heroes, nil
}
