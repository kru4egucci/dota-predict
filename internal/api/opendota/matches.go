package opendota

import (
	"context"
	"fmt"

	"dota-predict/internal/models"
)

// GetMatch fetches detailed match data by match ID (completed matches only).
func (c *Client) GetMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	var match models.Match
	if err := c.get(ctx, fmt.Sprintf("/matches/%d", matchID), &match); err != nil {
		return nil, fmt.Errorf("get match %d: %w", matchID, err)
	}
	return &match, nil
}

// GetLiveGames fetches all currently live Dota 2 games from OpenDota.
func (c *Client) GetLiveGames(ctx context.Context) ([]models.LiveGame, error) {
	var games []models.LiveGame
	if err := c.get(ctx, "/live", &games); err != nil {
		return nil, fmt.Errorf("get live games: %w", err)
	}
	return games, nil
}
