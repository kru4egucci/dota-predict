package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	MatchID      int64             `json:"match_id"`
	LobbyID      int64             `json:"lobby_id"`
	GameTime     int               `json:"game_time"`
	GameMode     int               `json:"game_mode"`
	LobbyType    int               `json:"lobby_type"`
	LeagueID     int               `json:"league_id"`
	RadiantScore int               `json:"radiant_series_wins"`
	DireScore    int               `json:"dire_series_wins"`
	RadiantTeam  steamTeam         `json:"radiant_team"`
	DireTeam     steamTeam         `json:"dire_team"`
	Players      []steamLivePlayer `json:"players"`
	Scoreboard   *steamScoreboard  `json:"scoreboard"`
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
	Duration float64        `json:"duration"`
	Radiant  steamTeamScore `json:"radiant"`
	Dire     steamTeamScore `json:"dire"`
}

type steamTeamScore struct {
	Score   int                `json:"score"`
	Players []steamPlayerScore `json:"players"`
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

// LiveLeagueGame holds exported data about a live league game from Steam API.
type LiveLeagueGame struct {
	MatchID         int64
	LeagueID        int
	RadiantTeamID   int
	DireTeamID      int
	RadiantTeamName string
	DireTeamName    string
	Players         []LiveLeaguePlayer
}

// LiveLeaguePlayer holds a player's info in a live league game.
type LiveLeaguePlayer struct {
	AccountID int
	HeroID    int
	Team      int // 0 = radiant, 1 = dire
}

// GetLiveLeagueGames returns all currently live league (tournament) games.
func (c *Client) GetLiveLeagueGames(ctx context.Context) ([]LiveLeagueGame, error) {
	start := time.Now()
	games, err := c.fetchLiveGames(ctx)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("steam: ошибка получения лайв-матчей",
			"duration", elapsed.String(),
			"error", err,
		)
		return nil, err
	}

	result := make([]LiveLeagueGame, 0, len(games))
	for _, g := range games {
		lg := LiveLeagueGame{
			MatchID:         g.MatchID,
			LeagueID:        g.LeagueID,
			RadiantTeamID:   g.RadiantTeam.TeamID,
			DireTeamID:      g.DireTeam.TeamID,
			RadiantTeamName: g.RadiantTeam.TeamName,
			DireTeamName:    g.DireTeam.TeamName,
		}

		// Prefer scoreboard players — the top-level Players array includes
		// admins, casters, and observers, not just the 10 actual players.
		if g.Scoreboard != nil {
			for _, p := range g.Scoreboard.Radiant.Players {
				lg.Players = append(lg.Players, LiveLeaguePlayer{
					AccountID: p.AccountID,
					HeroID:    p.HeroID,
					Team:      0,
				})
			}
			for _, p := range g.Scoreboard.Dire.Players {
				lg.Players = append(lg.Players, LiveLeaguePlayer{
					AccountID: p.AccountID,
					HeroID:    p.HeroID,
					Team:      1,
				})
			}
		} else {
			// Fallback: during draft phase scoreboard may not exist yet,
			// filter top-level players to team 0/1 only (skip spectators).
			for _, p := range g.Players {
				if p.Team == 0 || p.Team == 1 {
					lg.Players = append(lg.Players, LiveLeaguePlayer{
						AccountID: p.AccountID,
						HeroID:    p.HeroID,
						Team:      p.Team,
					})
				}
			}
		}

		result = append(result, lg)
	}

	slog.Debug("steam: лайв-матчи получены",
		"total_games", len(result),
		"duration", elapsed.String(),
	)

	return result, nil
}

// FindLiveMatch searches for a specific match among live league games.
func (c *Client) FindLiveMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	games, err := c.fetchLiveGames(ctx)
	if err != nil {
		return nil, err
	}

	for _, game := range games {
		if game.MatchID == matchID {
			slog.Debug("steam: лайв-матч найден",
				"match_id", matchID,
				"radiant", game.RadiantTeam.TeamName,
				"dire", game.DireTeam.TeamName,
			)
			return convertSteamLiveGame(&game), nil
		}
	}

	return nil, fmt.Errorf("матч %d не найден среди лайв лиговых матчей Steam", matchID)
}

func (c *Client) fetchLiveGames(ctx context.Context) ([]steamLiveGame, error) {
	url := fmt.Sprintf("%s/IDOTA2Match_570/GetLiveLeagueGames/v1/?key=%s", baseURL, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("requesting live league games: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("steam: неуспешный HTTP статус",
			"status", resp.StatusCode,
			"body", string(body),
			"duration", elapsed.String(),
		)
		return nil, fmt.Errorf("Steam API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result liveResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	slog.Debug("steam: API ответ получен",
		"games_count", len(result.Result.Games),
		"duration", elapsed.String(),
	)

	return result.Result.Games, nil
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

	// Prefer scoreboard players — the top-level Players array includes
	// admins, casters, and observers, not just the 10 actual players.
	if g.Scoreboard != nil {
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
	} else {
		// Fallback: during draft phase, filter out spectators (team >= 2).
		for _, p := range g.Players {
			if p.Team != 0 && p.Team != 1 {
				continue
			}
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
	}

	return m
}
