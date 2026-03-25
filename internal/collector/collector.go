package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"dota-predict/internal/api/opendota"
	"dota-predict/internal/api/steam"
	"dota-predict/internal/models"
)

// Collector orchestrates data collection from OpenDota and Steam APIs.
type Collector struct {
	od    *opendota.Client
	steam *steam.Client // may be nil
}

// New creates a new Collector.
func New(od *opendota.Client, steam *steam.Client) *Collector {
	return &Collector{od: od, steam: steam}
}

// CollectMatchData fetches all data needed for match prediction.
func (c *Collector) CollectMatchData(ctx context.Context, matchID int64) (*models.CollectedData, error) {
	log := slog.With("match_id", matchID)
	totalStart := time.Now()

	data := &models.CollectedData{
		HeroNames:       make(map[int]string),
		HeroPatchStats:  make(map[int]*models.HeroPatchStats),
		HeroLeagueStats: make(map[int]*models.HeroLeagueStats),
		HeroStats:    make(map[int]*models.HeroStats),
		HeroMatchups: make(map[int][]models.HeroMatchup),
		PlayerRecent: make(map[int][]models.PlayerRecentMatch),
	}

	// Шаг 1: Загрузка матча (OpenDota -> OpenDota Live -> Steam Live).
	log.Info("загрузка матча [1/4]")
	start := time.Now()
	match, err := c.fetchMatch(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("загрузка матча: %w", err)
	}
	data.Match = *match
	log.Info("матч загружен [1/4]",
		"duration", time.Since(start).String(),
		"players", len(match.Players),
	)

	// Извлекаем ID для параллельных запросов.
	radiantHeroes, direHeroes := splitHeroesByTeam(match)
	allHeroIDs := append(radiantHeroes, direHeroes...)

	radiantTeamID := match.RadiantTeam.TeamID
	direTeamID := match.DireTeam.TeamID
	isPro := radiantTeamID > 0 && direTeamID > 0

	if isPro {
		log.Info("сбор данных [2/4]",
			"radiant", match.RadiantTeam.Name,
			"dire", match.DireTeam.Name,
			"radiant_team_id", radiantTeamID,
			"dire_team_id", direTeamID,
			"hero_count", len(allHeroIDs),
		)
	} else {
		log.Info("сбор данных [2/4] — публичный матч",
			"hero_count", len(allHeroIDs),
		)
	}

	// Шаг 2: Параллельный сбор данных (ограничиваем параллелизм чтобы не превысить rate limit).
	start = time.Now()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	g.Go(func() error {
		heroes, err := c.od.GetHeroes(gctx)
		if err != nil {
			log.Warn("не удалось получить список героев", "error", err)
			return nil
		}
		for _, h := range heroes {
			data.HeroNames[h.ID] = h.LocalizedName
		}
		log.Debug("список героев загружен", "count", len(heroes))
		return nil
	})

	g.Go(func() error {
		stats, err := c.od.GetHeroStats(gctx)
		if err != nil {
			log.Warn("не удалось получить статистику героев", "error", err)
			return nil
		}
		for i, s := range stats {
			data.HeroStats[s.ID] = &stats[i]
		}
		log.Debug("статистика героев загружена", "count", len(stats))
		return nil
	})

	for _, heroID := range allHeroIDs {
		g.Go(func() error {
			matchups, err := c.od.GetHeroMatchups(gctx, heroID)
			if err != nil {
				log.Warn("не удалось получить матчапы героя", "hero_id", heroID, "error", err)
				return nil
			}
			data.HeroMatchups[heroID] = matchups
			return nil
		})
	}

	// Patch meta: fetch patch-specific hero stats.
	const currentPatch = "7.41"
	g.Go(func() error {
		data.Patch = currentPatch

		patchStats, err := c.od.GetHeroStatsByPatch(gctx, currentPatch)
		if err != nil {
			log.Warn("не удалось получить статистику патча", "patch", currentPatch, "error", err)
			return nil
		}
		for i, s := range patchStats {
			data.HeroPatchStats[s.HeroID] = &patchStats[i]
		}
		log.Debug("статистика патча загружена", "patch", currentPatch, "heroes", len(patchStats))
		return nil
	})

	// League meta: fetch tournament-specific hero stats if this is a league match.
	if data.Match.LeagueID > 0 {
		g.Go(func() error {
			leagueStats, err := c.od.GetHeroLeagueStats(gctx, data.Match.LeagueID)
			if err != nil {
				log.Warn("не удалось получить статистику турнира",
					"league_id", data.Match.LeagueID, "error", err)
				return nil
			}
			for i, s := range leagueStats {
				data.HeroLeagueStats[s.HeroID] = &leagueStats[i]
			}
			log.Debug("статистика турнира загружена",
				"league_id", data.Match.LeagueID, "heroes", len(leagueStats))
			return nil
		})
	}

	if isPro {
		g.Go(func() error {
			team, err := c.od.GetTeam(gctx, radiantTeamID)
			if err != nil {
				log.Warn("не удалось получить данные команды Radiant", "team_id", radiantTeamID, "error", err)
				return nil
			}
			data.RadiantTeam = team
			log.Debug("данные команды Radiant загружены", "team", team.Name)
			return nil
		})

		g.Go(func() error {
			team, err := c.od.GetTeam(gctx, direTeamID)
			if err != nil {
				log.Warn("не удалось получить данные команды Dire", "team_id", direTeamID, "error", err)
				return nil
			}
			data.DireTeam = team
			log.Debug("данные команды Dire загружены", "team", team.Name)
			return nil
		})

		g.Go(func() error {
			matches, err := c.od.GetTeamMatches(gctx, radiantTeamID)
			if err != nil {
				log.Warn("не удалось получить матчи Radiant", "team_id", radiantTeamID, "error", err)
				return nil
			}
			data.RadiantTeamMatches = matches
			log.Debug("матчи Radiant загружены", "count", len(matches))
			return nil
		})

		g.Go(func() error {
			matches, err := c.od.GetTeamMatches(gctx, direTeamID)
			if err != nil {
				log.Warn("не удалось получить матчи Dire", "team_id", direTeamID, "error", err)
				return nil
			}
			data.DireTeamMatches = matches
			log.Debug("матчи Dire загружены", "count", len(matches))
			return nil
		})

	}

	for _, player := range match.Players {
		if player.AccountID == 0 {
			continue
		}

		accountID := player.AccountID

		g.Go(func() error {
			recent, err := c.od.GetPlayerRecentMatches(gctx, accountID)
			if err != nil {
				log.Warn("не удалось получить последние матчи игрока", "account_id", accountID, "error", err)
				return nil
			}
			data.SetPlayerRecent(accountID, recent)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("сбор данных: %w", err)
	}

	log.Info("сбор данных завершён [3/4]",
		"duration", time.Since(start).String(),
		"total_duration", time.Since(totalStart).String(),
		"patch", data.Patch,
		"heroes_loaded", len(data.HeroNames),
		"hero_stats_loaded", len(data.HeroStats),
		"patch_stats_loaded", len(data.HeroPatchStats),
		"league_stats_loaded", len(data.HeroLeagueStats),
		"matchups_loaded", len(data.HeroMatchups),
	)

	return data, nil
}

// fetchMatch tries multiple sources: OpenDota history -> OpenDota live -> Steam live.
func (c *Collector) fetchMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	log := slog.With("match_id", matchID)

	// 1. OpenDota — завершённые матчи.
	log.Debug("поиск матча в истории OpenDota")
	match, err := c.od.GetMatch(ctx, matchID)
	if err == nil {
		log.Info("матч найден в истории OpenDota")
		return match, nil
	}
	log.Debug("матч не найден в истории OpenDota", "error", err)

	// 2. OpenDota /live.
	log.Debug("поиск среди лайв-матчей OpenDota")
	live, liveErr := c.od.GetLiveGames(ctx)
	if liveErr == nil {
		for i := range live {
			if int64(live[i].MatchID) == matchID {
				log.Info("матч найден среди лайв-матчей OpenDota")
				return live[i].ToMatch(), nil
			}
		}
		log.Debug("матч не найден среди лайв-матчей OpenDota", "live_games_checked", len(live))
	} else {
		log.Warn("ошибка получения лайв-матчей OpenDota", "error", liveErr)
	}

	// 3. Steam API (GetLiveLeagueGames).
	if c.steam != nil {
		log.Debug("поиск среди лайв лиговых матчей Steam")
		steamMatch, steamErr := c.steam.FindLiveMatch(ctx, matchID)
		if steamErr == nil {
			log.Info("матч найден среди лайв лиговых матчей Steam")
			return steamMatch, nil
		}
		log.Warn("матч не найден в Steam", "error", steamErr)
	} else {
		log.Debug("Steam API не настроен, пропускаю")
	}

	return nil, fmt.Errorf("матч %d не найден ни в одном источнике", matchID)
}

func splitHeroesByTeam(match *models.Match) (radiant, dire []int) {
	for _, p := range match.Players {
		if p.IsRadiant {
			radiant = append(radiant, p.HeroID)
		} else {
			dire = append(dire, p.HeroID)
		}
	}
	return
}

