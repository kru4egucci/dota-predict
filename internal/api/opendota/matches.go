package opendota

import (
	"context"
	"fmt"

	"dota-predict/internal/models"
)

// GetMatch fetches detailed match data by match ID.
func (c *Client) GetMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	var match models.Match
	if err := c.get(ctx, fmt.Sprintf("/matches/%d", matchID), &match); err != nil {
		return nil, fmt.Errorf("get match %d: %w", matchID, err)
	}
	return &match, nil
}
