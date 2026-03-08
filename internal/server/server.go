package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"dota-predict/internal/analyzer"
	"dota-predict/internal/api/oddspapi"
	"dota-predict/internal/api/opendota"
	"dota-predict/internal/api/openrouter"
	"dota-predict/internal/api/steam"
	"dota-predict/internal/api/telegram"
	"dota-predict/internal/collector"
	"dota-predict/internal/models"
	"dota-predict/internal/tier1"
)

const pollInterval = 30 * time.Second

// Server monitors live Dota 2 tournament matches via Steam API and sends predictions to Telegram.
type Server struct {
	odClient    *opendota.Client
	steamClient *steam.Client
	orClient    *openrouter.Client
	oddsClient  *oddspapi.Client
	tgClient    *telegram.Client
	coll        *collector.Collector
	ana         *analyzer.Analyzer

	mu        sync.Mutex
	processed map[int64]bool // match IDs already processed
}

// New creates a new Server.
func New(
	odClient *opendota.Client,
	steamClient *steam.Client,
	orClient *openrouter.Client,
	oddsClient *oddspapi.Client,
	tgClient *telegram.Client,
) *Server {
	return &Server{
		odClient:    odClient,
		steamClient: steamClient,
		orClient:    orClient,
		oddsClient:  oddsClient,
		tgClient:    tgClient,
		coll:        collector.New(odClient, steamClient),
		ana:         analyzer.New(orClient),
		processed:   make(map[int64]bool),
	}
}

// Run starts the polling loop. Blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	log.Println("[server] Запущен мониторинг тир-1 матчей через Steam API...")
	log.Printf("[server] Отслеживаемые команды: %v", tier1TeamNames())

	// Run immediately on start, then every pollInterval.
	s.tick(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[server] Остановка...")
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Server) tick(ctx context.Context) {
	games, err := s.steamClient.GetLiveLeagueGames(ctx)
	if err != nil {
		log.Printf("[server] Ошибка получения лайв-матчей Steam: %v", err)
		return
	}

	for i := range games {
		g := &games[i]

		if !tier1.HasTier1Team(g.RadiantTeamID, g.DireTeamID) {
			continue
		}

		if !isDraftComplete(g) {
			continue
		}

		s.mu.Lock()
		already := s.processed[g.MatchID]
		if !already {
			s.processed[g.MatchID] = true
		}
		s.mu.Unlock()

		if already {
			continue
		}

		log.Printf("[server] Найден тир-1 матч %d: %s vs %s — запуск анализа",
			g.MatchID, g.RadiantTeamName, g.DireTeamName)

		go s.processMatch(ctx, g)
	}
}

// isDraftComplete checks that all 10 players have heroes picked.
func isDraftComplete(g *steam.LiveLeagueGame) bool {
	if len(g.Players) < 10 {
		return false
	}
	for _, p := range g.Players {
		if p.HeroID == 0 {
			return false
		}
	}
	return true
}

// processMatch runs analysis and odds fetching in parallel, then sends Telegram notification.
func (s *Server) processMatch(ctx context.Context, game *steam.LiveLeagueGame) {
	matchID := game.MatchID
	radiantName := game.RadiantTeamName
	direName := game.DireTeamName

	type analysisResult struct {
		prediction *models.Prediction
		err        error
	}
	type oddsResult struct {
		odds *models.MatchOdds
		err  error
	}

	analysisCh := make(chan analysisResult, 1)
	oddsCh := make(chan oddsResult, 1)

	// Run analysis.
	go func() {
		data, err := s.coll.CollectMatchData(ctx, matchID)
		if err != nil {
			analysisCh <- analysisResult{err: fmt.Errorf("сбор данных: %w", err)}
			return
		}
		pred, err := s.ana.Predict(ctx, data)
		if err != nil {
			analysisCh <- analysisResult{err: fmt.Errorf("анализ: %w", err)}
			return
		}
		analysisCh <- analysisResult{prediction: pred}
	}()

	// Fetch odds in parallel.
	go func() {
		if s.oddsClient == nil {
			oddsCh <- oddsResult{err: fmt.Errorf("OddsPapi не настроен")}
			return
		}
		odds, err := s.oddsClient.FindMatchOdds(ctx, radiantName, direName)
		oddsCh <- oddsResult{odds: odds, err: err}
	}()

	aRes := <-analysisCh
	oRes := <-oddsCh

	if aRes.err != nil {
		log.Printf("[server] Ошибка анализа матча %d: %v", matchID, aRes.err)
		s.sendError(ctx, matchID, radiantName, direName, aRes.err)
		return
	}

	prediction := aRes.prediction

	if oRes.err != nil {
		log.Printf("[server] Не удалось получить коэффициенты для матча %d: %v", matchID, oRes.err)
	}

	msg := s.buildMessage(prediction, oRes.odds, matchID)

	if err := s.tgClient.SendMessage(ctx, msg); err != nil {
		log.Printf("[server] Ошибка отправки в Telegram для матча %d: %v", matchID, err)
	} else {
		log.Printf("[server] Уведомление отправлено для матча %d", matchID)
	}
}

// buildMessage creates the Telegram message based on analysis and odds.
func (s *Server) buildMessage(pred *models.Prediction, odds *models.MatchOdds, matchID int64) string {
	var sb strings.Builder
	betting := &pred.Betting

	hasBet := false
	var betTeam string
	var betOdds float64
	var comfortOdds float64

	if odds != nil && betting.RadiantWinProb > 0 {
		// Check if any bookmaker odds are >= comfortable odds (value bet).
		radiantBookOdds := matchOddsForTeam(odds, pred.RadiantTeamName)
		direBookOdds := matchOddsForTeam(odds, pred.DireTeamName)

		if radiantBookOdds > 0 && betting.RadiantComfortOdds > 0 && radiantBookOdds >= betting.RadiantComfortOdds {
			hasBet = true
			betTeam = pred.RadiantTeamName
			betOdds = radiantBookOdds
			comfortOdds = betting.RadiantComfortOdds
		} else if direBookOdds > 0 && betting.DireComfortOdds > 0 && direBookOdds >= betting.DireComfortOdds {
			hasBet = true
			betTeam = pred.DireTeamName
			betOdds = direBookOdds
			comfortOdds = betting.DireComfortOdds
		}
	}

	if hasBet {
		sb.WriteString(fmt.Sprintf("<b>СТАВКА: %s vs %s</b>\n", pred.RadiantTeamName, pred.DireTeamName))
		sb.WriteString(fmt.Sprintf("Match ID: %d\n\n", matchID))
		sb.WriteString(fmt.Sprintf("Ставка на: <b>%s</b>\n", betTeam))
		sb.WriteString(fmt.Sprintf("Коэффициент у букмекера: <b>%.2f</b>\n", betOdds))
		sb.WriteString(fmt.Sprintf("Комфортный коэффициент: %.2f\n", comfortOdds))
		if odds != nil {
			sb.WriteString(fmt.Sprintf("Букмекер: %s\n", odds.Bookmaker))
		}
		sb.WriteString(fmt.Sprintf("\nВероятность победы %s: %.1f%%\n", pred.RadiantTeamName, betting.RadiantWinProb))
		sb.WriteString(fmt.Sprintf("Вероятность победы %s: %.1f%%\n", pred.DireTeamName, betting.DireWinProb))
		if betting.Confidence != "" {
			sb.WriteString(fmt.Sprintf("Уверенность: %s\n", strings.ToUpper(betting.Confidence)))
		}
	} else {
		sb.WriteString(fmt.Sprintf("<b>АНАЛИТИКА: %s vs %s</b>\n", pred.RadiantTeamName, pred.DireTeamName))
		sb.WriteString(fmt.Sprintf("Match ID: %d\n", matchID))
		sb.WriteString("\nСтавка не рекомендована (коэффициенты ниже комфортных)\n")
	}

	// Odds info.
	sb.WriteString("\n<b>Коэффициенты:</b>\n")
	if odds != nil {
		sb.WriteString(fmt.Sprintf("  %s: %.2f (%s)\n", odds.Team1Name, odds.Team1Odds, odds.Bookmaker))
		sb.WriteString(fmt.Sprintf("  %s: %.2f (%s)\n", odds.Team2Name, odds.Team2Odds, odds.Bookmaker))
	} else {
		sb.WriteString("  Не удалось получить коэффициенты букмекеров\n")
	}

	sb.WriteString("\n<b>Расчётные коэффициенты:</b>\n")
	if betting.RadiantMinOdds > 0 {
		sb.WriteString(fmt.Sprintf("  %s: мин %.2f / комфорт %.2f\n",
			pred.RadiantTeamName, betting.RadiantMinOdds, betting.RadiantComfortOdds))
	}
	if betting.DireMinOdds > 0 {
		sb.WriteString(fmt.Sprintf("  %s: мин %.2f / комфорт %.2f\n",
			pred.DireTeamName, betting.DireMinOdds, betting.DireComfortOdds))
	}

	// Draft analysis summary (if available, keep it short).
	if pred.DraftAnalysis != "" && betting.DraftRadiantProb > 0 {
		sb.WriteString(fmt.Sprintf("\n<b>Драфт:</b> %s %.1f%% — %s %.1f%%\n",
			pred.RadiantTeamName, betting.DraftRadiantProb,
			pred.DireTeamName, betting.DraftDireProb))
	}

	// Truncated analysis (first ~1500 chars to keep message readable).
	sb.WriteString("\n<b>Анализ:</b>\n")
	analysis := pred.Analysis
	if len(analysis) > 1500 {
		analysis = analysis[:1500] + "..."
	}
	analysis = telegram.MDToTelegramHTML(analysis)
	sb.WriteString(analysis)

	return sb.String()
}

// matchOddsForTeam finds the bookmaker odds for a specific team by fuzzy name matching.
func matchOddsForTeam(odds *models.MatchOdds, teamName string) float64 {
	t := strings.ToLower(teamName)
	if fuzzyContains(strings.ToLower(odds.Team1Name), t) {
		return odds.Team1Odds
	}
	if fuzzyContains(strings.ToLower(odds.Team2Name), t) {
		return odds.Team2Odds
	}
	return 0
}

func fuzzyContains(a, b string) bool {
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return true
	}
	strip := func(s string) string {
		for _, w := range []string{"team ", "esports", "gaming"} {
			s = strings.ReplaceAll(s, w, "")
		}
		return strings.TrimSpace(s)
	}
	a2 := strip(a)
	b2 := strip(b)
	return a2 == b2 || strings.Contains(a2, b2) || strings.Contains(b2, a2)
}

func (s *Server) sendError(ctx context.Context, matchID int64, radiant, dire string, err error) {
	msg := fmt.Sprintf("<b>ОШИБКА: %s vs %s</b>\nMatch ID: %d\n\n%v", radiant, dire, matchID, err)
	if sendErr := s.tgClient.SendMessage(ctx, msg); sendErr != nil {
		log.Printf("[server] Не удалось отправить ошибку в Telegram: %v", sendErr)
	}
}

func tier1TeamNames() []string {
	names := make([]string, 0, len(tier1.Teams))
	for _, name := range tier1.Teams {
		names = append(names, name)
	}
	return names
}
