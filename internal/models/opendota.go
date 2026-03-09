package models

import (
	"encoding/json"
	"strconv"
)

// jsonInt64 unmarshals from both JSON number and JSON string.
type jsonInt64 int64

func (j *jsonInt64) UnmarshalJSON(data []byte) error {
	// Try number first.
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*j = jsonInt64(n)
		return nil
	}
	// Try string.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*j = jsonInt64(n)
	return nil
}

// Match represents the GET /matches/{match_id} response.
type Match struct {
	MatchID      int64         `json:"match_id"`
	Duration     int           `json:"duration"`
	RadiantWin   bool          `json:"radiant_win"`
	RadiantScore int           `json:"radiant_score"`
	DireScore    int           `json:"dire_score"`
	RadiantTeam  MatchTeam     `json:"radiant_team"`
	DireTeam     MatchTeam     `json:"dire_team"`
	Players      []MatchPlayer `json:"players"`
	PicksBans    []PickBan     `json:"picks_bans"`
	StartTime    int64         `json:"start_time"`
	GameMode     int           `json:"game_mode"`
	LobbyType    int           `json:"lobby_type"`
	LeagueID     int           `json:"leagueid"`
	Patch        int           `json:"patch"`
}

// MatchTeam is team info embedded in a match response.
type MatchTeam struct {
	TeamID  int    `json:"team_id"`
	Name    string `json:"name"`
	Tag     string `json:"tag"`
	LogoURL string `json:"logo_url"`
}

// MatchPlayer is per-player data within a match.
type MatchPlayer struct {
	AccountID   int    `json:"account_id"`
	PlayerSlot  int    `json:"player_slot"`
	HeroID      int    `json:"hero_id"`
	Name        string `json:"name"`
	Personaname string `json:"personaname"`
	IsRadiant   bool   `json:"isRadiant"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
	Assists     int    `json:"assists"`
	GoldPerMin  int    `json:"gold_per_min"`
	XpPerMin    int    `json:"xp_per_min"`
	NetWorth    int    `json:"net_worth"`
	HeroDamage  int    `json:"hero_damage"`
	TowerDamage int    `json:"tower_damage"`
	HeroHealing int    `json:"hero_healing"`
	LastHits    int    `json:"last_hits"`
	Denies      int    `json:"denies"`
	Level       int    `json:"level"`
}

// PickBan represents a single pick or ban in the draft.
type PickBan struct {
	IsPick bool `json:"is_pick"`
	HeroID int  `json:"hero_id"`
	Team   int  `json:"team"`  // 0 = radiant, 1 = dire
	Order  int  `json:"order"`
}

// Hero represents GET /heroes response entry.
type Hero struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	LocalizedName string   `json:"localized_name"`
	PrimaryAttr   string   `json:"primary_attr"`
	AttackType    string   `json:"attack_type"`
	Roles         []string `json:"roles"`
}

// HeroStats represents GET /heroStats response entry.
type HeroStats struct {
	ID            int    `json:"id"`
	LocalizedName string `json:"localized_name"`
	ProPick       int    `json:"pro_pick"`
	ProWin        int    `json:"pro_win"`
	ProBan        int    `json:"pro_ban"`
	PrimaryAttr   string `json:"primary_attr"`
	AttackType    string `json:"attack_type"`

	// Per-bracket pick/win counts (1=Herald .. 8=Immortal).
	Bracket1Pick int `json:"1_pick"`
	Bracket1Win  int `json:"1_win"`
	Bracket2Pick int `json:"2_pick"`
	Bracket2Win  int `json:"2_win"`
	Bracket3Pick int `json:"3_pick"`
	Bracket3Win  int `json:"3_win"`
	Bracket4Pick int `json:"4_pick"`
	Bracket4Win  int `json:"4_win"`
	Bracket5Pick int `json:"5_pick"`
	Bracket5Win  int `json:"5_win"`
	Bracket6Pick int `json:"6_pick"`
	Bracket6Win  int `json:"6_win"`
	Bracket7Pick int `json:"7_pick"`
	Bracket7Win  int `json:"7_win"`
	Bracket8Pick int `json:"8_pick"`
	Bracket8Win  int `json:"8_win"`
}

// HeroMatchup represents GET /heroes/{hero_id}/matchups response entry.
type HeroMatchup struct {
	HeroID      int `json:"hero_id"`
	GamesPlayed int `json:"games_played"`
	Wins        int `json:"wins"`
}

// Team represents GET /teams/{team_id} response.
type Team struct {
	TeamID        int     `json:"team_id"`
	Rating        float64 `json:"rating"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	Name          string  `json:"name"`
	Tag           string  `json:"tag"`
	LogoURL       string  `json:"logo_url"`
	LastMatchTime int64   `json:"last_match_time"`
}

// TeamMatch represents GET /teams/{team_id}/matches response entry.
type TeamMatch struct {
	MatchID          int64  `json:"match_id"`
	RadiantWin       bool   `json:"radiant_win"`
	Radiant          bool   `json:"radiant"`
	RadiantScore     int    `json:"radiant_score"`
	DireScore        int    `json:"dire_score"`
	Duration         int    `json:"duration"`
	StartTime        int64  `json:"start_time"`
	LeagueID         int    `json:"leagueid"`
	LeagueName       string `json:"league_name"`
	OpposingTeamID   int    `json:"opposing_team_id"`
	OpposingTeamName string `json:"opposing_team_name"`
}

// TeamHero represents GET /teams/{team_id}/heroes response entry.
type TeamHero struct {
	HeroID        int    `json:"hero_id"`
	LocalizedName string `json:"localized_name"`
	GamesPlayed   int    `json:"games_played"`
	Wins          int    `json:"wins"`
}

// TeamPlayer represents GET /teams/{team_id}/players response entry.
type TeamPlayer struct {
	AccountID           int    `json:"account_id"`
	Name                string `json:"name"`
	GamesPlayed         int    `json:"games_played"`
	Wins                int    `json:"wins"`
	IsCurrentTeamMember bool   `json:"is_current_team_member"`
}

// LiveGame represents an entry from GET /live response.
// Some numeric fields come as strings from the API, so we use json.Number / string where needed.
type LiveGame struct {
	MatchID          jsonInt64    `json:"match_id"`
	LobbyID          string       `json:"lobby_id"`
	LobbyType        int          `json:"lobby_type"`
	GameTime         int          `json:"game_time"`
	GameMode         int          `json:"game_mode"`
	AverageMMR       int          `json:"average_mmr"`
	RadiantScore     int          `json:"radiant_score"`
	DireScore        int          `json:"dire_score"`
	RadiantLead      int          `json:"radiant_lead"`
	SortScore        float64      `json:"sort_score"`
	Players          []LivePlayer `json:"players"`
	RadiantTeamName  string       `json:"team_name_radiant"`
	DireTeamName     string       `json:"team_name_dire"`
	RadiantTeamID    int          `json:"team_id_radiant"`
	DireTeamID       int          `json:"team_id_dire"`
	BuildingState    uint64       `json:"building_state"`
}

// LivePlayer represents a player in a live game.
type LivePlayer struct {
	AccountID int `json:"account_id"`
	HeroID    int `json:"hero_id"`
	Team      int `json:"team"` // 0 = radiant, 1 = dire
}

// ToMatch converts a LiveGame into a Match for unified processing.
func (lg *LiveGame) ToMatch() *Match {
	m := &Match{
		MatchID:      int64(lg.MatchID),
		GameMode:     lg.GameMode,
		LobbyType:    lg.LobbyType,
		RadiantScore: lg.RadiantScore,
		DireScore:    lg.DireScore,
		RadiantTeam: MatchTeam{
			TeamID: lg.RadiantTeamID,
			Name:   lg.RadiantTeamName,
		},
		DireTeam: MatchTeam{
			TeamID: lg.DireTeamID,
			Name:   lg.DireTeamName,
		},
	}

	for _, p := range lg.Players {
		isRadiant := p.Team == 0
		slot := 0
		if !isRadiant {
			slot = 128
		}
		m.Players = append(m.Players, MatchPlayer{
			AccountID:  p.AccountID,
			PlayerSlot: slot,
			HeroID:     p.HeroID,
			IsRadiant:  isRadiant,
		})
	}

	return m
}

// HeroPatchStats represents hero win rate statistics for a specific game patch
// (sourced from the Explorer SQL endpoint).
type HeroPatchStats struct {
	HeroID int
	Games  int
	Wins   int
}

// HeroLeagueStats represents hero pick/ban/win statistics for a specific league
// (sourced from the Explorer SQL endpoint).
type HeroLeagueStats struct {
	HeroID int
	Picks  int
	Bans   int
	Wins   int // wins out of Picks
}

// PlayerHeroStat represents GET /players/{account_id}/heroes response entry.
type PlayerHeroStat struct {
	HeroID     int   `json:"hero_id"`
	LastPlayed int64 `json:"last_played"`
	Games      int   `json:"games"`
	Win        int   `json:"win"`
}

// PlayerRecentMatch represents GET /players/{account_id}/recentMatches entry.
type PlayerRecentMatch struct {
	MatchID    int64 `json:"match_id"`
	PlayerSlot int   `json:"player_slot"`
	RadiantWin bool  `json:"radiant_win"`
	HeroID     int   `json:"hero_id"`
	Duration   int   `json:"duration"`
	GameMode   int   `json:"game_mode"`
	Kills      int   `json:"kills"`
	Deaths     int   `json:"deaths"`
	Assists    int   `json:"assists"`
	XpPerMin   int   `json:"xp_per_min"`
	GoldPerMin int   `json:"gold_per_min"`
	HeroDamage int   `json:"hero_damage"`
	LastHits   int   `json:"last_hits"`
	StartTime  int64 `json:"start_time"`
	LaneRole   int   `json:"lane_role"` // 1=safe lane, 2=mid, 3=off lane, 4=jungle
	Lane       int   `json:"lane"`      // 1=bot, 2=mid, 3=top
}
