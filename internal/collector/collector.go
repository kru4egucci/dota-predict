package collector

import (
	"context"
	"fmt"
	"log"

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
	data := &models.CollectedData{
		HeroNames:       make(map[int]string),
		HeroStats:       make(map[int]*models.HeroStats),
		HeroMatchups:    make(map[int][]models.HeroMatchup),
		PlayerHeroStats: make(map[int]*models.PlayerHeroStat),
		PlayerRecent:    make(map[int][]models.PlayerRecentMatch),
	}

	// Шаг 1: Загрузка матча (OpenDota -> OpenDota Live -> Steam Live).
	fmt.Printf("[1/4] Загрузка матча %d...\n", matchID)
	match, err := c.fetchMatch(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("загрузка матча: %w", err)
	}
	data.Match = *match

	// Извлекаем ID для параллельных запросов.
	radiantHeroes, direHeroes := splitHeroesByTeam(match)
	allHeroIDs := append(radiantHeroes, direHeroes...)

	radiantTeamID := match.RadiantTeam.TeamID
	direTeamID := match.DireTeam.TeamID
	isPro := radiantTeamID > 0 && direTeamID > 0

	if isPro {
		fmt.Printf("[2/4] Матч: %s vs %s — сбор данных...\n", match.RadiantTeam.Name, match.DireTeam.Name)
	} else {
		fmt.Println("[2/4] Публичный матч — сбор данных по героям и игрокам...")
	}

	// Шаг 2: Параллельный сбор данных (ограничиваем параллелизм чтобы не превысить rate limit).
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	g.Go(func() error {
		heroes, err := c.od.GetHeroes(gctx)
		if err != nil {
			log.Printf("  [!] не удалось получить список героев: %v", err)
			return nil
		}
		for _, h := range heroes {
			data.HeroNames[h.ID] = h.LocalizedName
		}
		return nil
	})

	g.Go(func() error {
		stats, err := c.od.GetHeroStats(gctx)
		if err != nil {
			log.Printf("  [!] не удалось получить статистику героев: %v", err)
			return nil
		}
		for i, s := range stats {
			data.HeroStats[s.ID] = &stats[i]
		}
		return nil
	})

	for _, heroID := range allHeroIDs {
		g.Go(func() error {
			matchups, err := c.od.GetHeroMatchups(gctx, heroID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчапы героя %d: %v", heroID, err)
				return nil
			}
			data.HeroMatchups[heroID] = matchups
			return nil
		})
	}

	if isPro {
		g.Go(func() error {
			team, err := c.od.GetTeam(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить данные команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeam = team
			return nil
		})

		g.Go(func() error {
			team, err := c.od.GetTeam(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить данные команды Dire: %v", err)
				return nil
			}
			data.DireTeam = team
			return nil
		})

		g.Go(func() error {
			matches, err := c.od.GetTeamMatches(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчи команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeamMatches = matches
			return nil
		})

		g.Go(func() error {
			matches, err := c.od.GetTeamMatches(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчи команды Dire: %v", err)
				return nil
			}
			data.DireTeamMatches = matches
			return nil
		})

		g.Go(func() error {
			heroes, err := c.od.GetTeamHeroes(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить героев команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeamHeroes = heroes
			return nil
		})

		g.Go(func() error {
			heroes, err := c.od.GetTeamHeroes(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить героев команды Dire: %v", err)
				return nil
			}
			data.DireTeamHeroes = heroes
			return nil
		})
	}

	for _, player := range match.Players {
		if player.AccountID == 0 {
			continue
		}

		accountID := player.AccountID
		heroID := player.HeroID

		g.Go(func() error {
			heroStats, err := c.od.GetPlayerHeroes(gctx, accountID)
			if err != nil {
				log.Printf("  [!] не удалось получить героев игрока %d: %v", accountID, err)
				return nil
			}
			for i, hs := range heroStats {
				if hs.HeroID == heroID {
					data.SetPlayerHeroStat(accountID, &heroStats[i])
					break
				}
			}
			return nil
		})

		g.Go(func() error {
			recent, err := c.od.GetPlayerRecentMatches(gctx, accountID)
			if err != nil {
				log.Printf("  [!] не удалось получить последние матчи игрока %d: %v", accountID, err)
				return nil
			}
			data.SetPlayerRecent(accountID, recent)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("сбор данных: %w", err)
	}

	if isPro && len(data.RadiantTeamMatches) > 0 {
		data.HeadToHead = filterH2H(data.RadiantTeamMatches, direTeamID)
	}

	fmt.Println("[3/4] Сбор данных завершён.")

	return data, nil
}

// fetchMatch tries multiple sources: OpenDota history -> OpenDota live -> Steam live.
func (c *Collector) fetchMatch(ctx context.Context, matchID int64) (*models.Match, error) {
	// 1. OpenDota — завершённые матчи.
	match, err := c.od.GetMatch(ctx, matchID)
	if err == nil {
		return match, nil
	}
	fmt.Printf("  Матч не найден в истории OpenDota, ищу среди лайв-матчей...\n")

	// 2. OpenDota /live.
	live, liveErr := c.od.GetLiveGames(ctx)
	if liveErr == nil {
		for i := range live {
			if int64(live[i].MatchID) == matchID {
				fmt.Println("  Лайв-матч найден (OpenDota)!")
				return live[i].ToMatch(), nil
			}
		}
	}

	// 3. Steam API (GetLiveLeagueGames).
	if c.steam != nil {
		fmt.Println("  Ищу среди лайв лиговых матчей Steam...")
		steamMatch, steamErr := c.steam.FindLiveMatch(ctx, matchID)
		if steamErr == nil {
			fmt.Println("  Лайв-матч найден (Steam)!")
			return steamMatch, nil
		}
		log.Printf("  [!] Steam: %v", steamErr)
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

func filterH2H(teamMatches []models.TeamMatch, opposingTeamID int) []models.TeamMatch {
	var h2h []models.TeamMatch
	for _, m := range teamMatches {
		if m.OpposingTeamID == opposingTeamID {
			h2h = append(h2h, m)
		}
	}
	return h2h
}
