package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dota-predict/internal/models"
)

const baseURL = "https://api.steampowered.com"

// Client is a Steam Web API client for Dota 2.
type Client struct {
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a new Steam API client. Returns nil if apiKey is empty.
func NewClient(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiKey:     apiKey,
	}
}

// liveResponse is the raw Steam API response wrapper.
type liveResponse struct {
	Result struct {
		Games []steamLiveGame `json:"games"`
	} `json:"result"`
}

type steamLiveGame struct {
	MatchID      int64              `json:"match_id"`
	LobbyID      int64              `json:"lobby_id"`
	GameTime     int                `json:"game_time"`
	GameMode     int                `json:"game_mode"`
	LobbyType    int                `json:"lobby_type"`
	LeagueID     int                `json:"league_id"`
	RadiantScore int                `json:"radiant_series_wins"`
	DireScore    int                `json:"dire_series_wins"`
	RadiantTeam  steamTeam          `json:"radiant_team"`
	DireTeam     steamTeam          `json:"dire_team"`
	Players      []steamLivePlayer  `json:"players"`
	Scoreboard   *steamScoreboard   `json:"scoreboard"`
}

type steamTeam struct {
	TeamName string `json:"team_name"`
	TeamID   int    `json:"team_id"`
	TeamLogo uint64 `json:"team_logo"`
}

type steamLivePlayer struct {
	AccountID int    `json:"account_id"`
	Name      string `json:"name"`
	HeroID    int    `json:"hero_id"`
	Team      int    `json:"team"` // 0 = radiant, 1 = dire
}

type steamScoreboard struct {
	Duration float64          `json:"duration"`
	Radiant  steamTeamScore   `json:"radiant"`
	Dire     steamTeamScore   `json:"dire"`
}

type steamTeamScore struct {
	Score   int                   `json:"score"`
	Players []steamPlayerScore    `json:"players"`
}

type steamPlayerScore struct {
	AccountID int `json:"account_id"`
	HeroID    int `json:"hero_id"`
	Kills     int `json:"kills"`
	Deaths    int `json:"death"`
	Assists   int `json:"assists"`
	NetWorth  int `json:"net_worth"`
	Level     int `json:"level"`
}

// FindLiveMatch searches for a specific match among live league games.
func (c *Client) FindLiveMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	url := fmt.Sprintf("%s/IDOTA2Match_570/GetLiveLeagueGames/v1/?key=%s", baseURL, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting live league games: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Steam API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result liveResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	for _, game := range result.Result.Games {
		if game.MatchID == matchID {
			return convertSteamLiveGame(&game), nil
		}
	}

	return nil, fmt.Errorf("матч %d не найден среди лайв лиговых матчей Steam", matchID)
}

func convertSteamLiveGame(g *steamLiveGame) *models.Match {
	m := &models.Match{
		MatchID:  g.MatchID,
		GameMode: g.GameMode,
		LobbyType: g.LobbyType,
		LeagueID: g.LeagueID,
		RadiantTeam: models.MatchTeam{
			TeamID: g.RadiantTeam.TeamID,
			Name:   g.RadiantTeam.TeamName,
		},
		DireTeam: models.MatchTeam{
			TeamID: g.DireTeam.TeamID,
			Name:   g.DireTeam.TeamName,
		},
	}

	if g.Scoreboard != nil {
		m.Duration = int(g.Scoreboard.Duration)
		m.RadiantScore = g.Scoreboard.Radiant.Score
		m.DireScore = g.Scoreboard.Dire.Score
	}

	for _, p := range g.Players {
		isRadiant := p.Team == 0
		slot := 0
		if !isRadiant {
			slot = 128
		}
		m.Players = append(m.Players, models.MatchPlayer{
			AccountID:  p.AccountID,
			PlayerSlot: slot,
			HeroID:     p.HeroID,
			Name:       p.Name,
			IsRadiant:  isRadiant,
		})
	}

	// If players came without hero_id (draft phase), try scoreboard.
	if g.Scoreboard != nil && len(m.Players) == 0 {
		for _, p := range g.Scoreboard.Radiant.Players {
			m.Players = append(m.Players, models.MatchPlayer{
				AccountID: p.AccountID,
				HeroID:    p.HeroID,
				IsRadiant: true,
			})
		}
		for _, p := range g.Scoreboard.Dire.Players {
			m.Players = append(m.Players, models.MatchPlayer{
				AccountID:  p.AccountID,
				HeroID:     p.HeroID,
				PlayerSlot: 128,
				IsRadiant:  false,
			})
		}
	}

	return m
}
