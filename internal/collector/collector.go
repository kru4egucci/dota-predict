package collector

import (
	"context"
	"fmt"
	"log"

	"golang.org/x/sync/errgroup"

	"dota-predict/internal/api/opendota"
	"dota-predict/internal/models"
)

// Collector orchestrates data collection from OpenDota API.
type Collector struct {
	client *opendota.Client
}

// New creates a new Collector.
func New(client *opendota.Client) *Collector {
	return &Collector{client: client}
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

	// Шаг 1: Загрузка данных матча.
	fmt.Printf("[1/4] Загрузка матча %d...\n", matchID)
	match, err := c.client.GetMatch(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("fetching match: %w", err)
	}
	data.Match = *match

	// Extract IDs for concurrent fetching.
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

	// Step 2: Concurrent data fetching.
	g, gctx := errgroup.WithContext(ctx)

	// Hero names.
	g.Go(func() error {
		heroes, err := c.client.GetHeroes(gctx)
		if err != nil {
			log.Printf("  [!] не удалось получить список героев: %v", err)
			return nil
		}
		for _, h := range heroes {
			data.HeroNames[h.ID] = h.LocalizedName
		}
		return nil
	})

	// Hero stats.
	g.Go(func() error {
		stats, err := c.client.GetHeroStats(gctx)
		if err != nil {
			log.Printf("  [!] не удалось получить статистику героев: %v", err)
			return nil
		}
		for i, s := range stats {
			data.HeroStats[s.ID] = &stats[i]
		}
		return nil
	})

	// Hero matchups (one request per hero in the match).
	for _, heroID := range allHeroIDs {
		g.Go(func() error {
			matchups, err := c.client.GetHeroMatchups(gctx, heroID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчапы героя %d: %v", heroID, err)
				return nil
			}
			data.HeroMatchups[heroID] = matchups
			return nil
		})
	}

	// Team data (pro matches only).
	if isPro {
		g.Go(func() error {
			team, err := c.client.GetTeam(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить данные команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeam = team
			return nil
		})

		g.Go(func() error {
			team, err := c.client.GetTeam(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить данные команды Dire: %v", err)
				return nil
			}
			data.DireTeam = team
			return nil
		})

		g.Go(func() error {
			matches, err := c.client.GetTeamMatches(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчи команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeamMatches = matches
			return nil
		})

		g.Go(func() error {
			matches, err := c.client.GetTeamMatches(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить матчи команды Dire: %v", err)
				return nil
			}
			data.DireTeamMatches = matches
			return nil
		})

		g.Go(func() error {
			heroes, err := c.client.GetTeamHeroes(gctx, radiantTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить героев команды Radiant: %v", err)
				return nil
			}
			data.RadiantTeamHeroes = heroes
			return nil
		})

		g.Go(func() error {
			heroes, err := c.client.GetTeamHeroes(gctx, direTeamID)
			if err != nil {
				log.Printf("  [!] не удалось получить героев команды Dire: %v", err)
				return nil
			}
			data.DireTeamHeroes = heroes
			return nil
		})
	}

	// Player hero comfort and recent form.
	for _, player := range match.Players {
		if player.AccountID == 0 {
			continue
		}

		accountID := player.AccountID
		heroID := player.HeroID

		g.Go(func() error {
			heroStats, err := c.client.GetPlayerHeroes(gctx, accountID)
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
			recent, err := c.client.GetPlayerRecentMatches(gctx, accountID)
			if err != nil {
				log.Printf("  [!] не удалось получить последние матчи игрока %d: %v", accountID, err)
				return nil
			}
			data.SetPlayerRecent(accountID, recent)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("collecting data: %w", err)
	}

	// Step 3: Post-processing — extract H2H.
	if isPro && len(data.RadiantTeamMatches) > 0 {
		data.HeadToHead = filterH2H(data.RadiantTeamMatches, direTeamID)
	}

	fmt.Println("[3/4] Сбор данных завершён.")

	return data, nil
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
