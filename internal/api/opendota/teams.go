package opendota

import (
	"context"
	"fmt"

	"dota-predict/internal/models"
)

// GetTeam fetches team info including overall win/loss record.
func (c *Client) GetTeam(ctx context.Context, teamID int) (*models.Team, error) {
	var team models.Team
	if err := c.get(ctx, fmt.Sprintf("/teams/%d", teamID), &team); err != nil {
		return nil, fmt.Errorf("get team %d: %w", teamID, err)
	}
	return &team, nil
}

// GetTeamMatches fetches a team's match history.
func (c *Client) GetTeamMatches(ctx context.Context, teamID int) ([]models.TeamMatch, error) {
	var matches []models.TeamMatch
	if err := c.get(ctx, fmt.Sprintf("/teams/%d/matches", teamID), &matches); err != nil {
		return nil, fmt.Errorf("get team %d matches: %w", teamID, err)
	}
	return matches, nil
}

// GetTeamHeroes fetches hero-specific win rates for a team.
func (c *Client) GetTeamHeroes(ctx context.Context, teamID int) ([]models.TeamHero, error) {
	var heroes []models.TeamHero
	if err := c.get(ctx, fmt.Sprintf("/teams/%d/heroes", teamID), &heroes); err != nil {
		return nil, fmt.Errorf("get team %d heroes: %w", teamID, err)
	}
	return heroes, nil
}
