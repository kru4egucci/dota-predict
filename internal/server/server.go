package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"dota-predict/internal/analyzer"
	"dota-predict/internal/api/gsheets"
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
	odClient      *opendota.Client
	steamClient   *steam.Client
	orClient      *openrouter.Client
	oddsClient    *oddspapi.Client
	tgClient      *telegram.Client
	gsheetsClient *gsheets.Client
	coll          *collector.Collector
	ana           *analyzer.Analyzer

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
	gsheetsClient *gsheets.Client,
) *Server {
	return &Server{
		odClient:      odClient,
		steamClient:   steamClient,
		orClient:      orClient,
		oddsClient:    oddsClient,
		tgClient:      tgClient,
		gsheetsClient: gsheetsClient,
		coll:          collector.New(odClient, steamClient),
		ana:           analyzer.New(orClient),
		processed:     make(map[int64]bool),
	}
}

// Run starts the polling loop. Blocks until context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	teamNames := tier1TeamNames()
	slog.Info("сервер запущен",
		"poll_interval", pollInterval.String(),
		"tracked_teams_count", len(teamNames),
		"tracked_teams", teamNames,
	)

	// Start periodic odds refresh (every hour).
	if s.oddsClient != nil {
		s.oddsClient.StartPeriodicRefresh(ctx)
	}

	// Start hourly result checker for Google Sheets.
	s.startResultChecker(ctx)

	// Run immediately on start, then every pollInterval.
	s.tick(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("сервер остановлен", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Server) tick(ctx context.Context) {
	slog.Debug("опрос Steam API")

	start := time.Now()
	games, err := s.steamClient.GetLiveLeagueGames(ctx)
	elapsed := time.Since(start)

	if err != nil {
		slog.Error("ошибка получения лайв-матчей Steam",
			"error", err,
			"duration", elapsed.String(),
		)
		return
	}

	slog.Debug("получены лайв-матчи",
		"total_games", len(games),
		"duration", elapsed.String(),
	)

	for i := range games {
		g := &games[i]

		if !tier1.HasTier1Team(g.RadiantTeamID, g.DireTeamID) {
			continue
		}

		log := slog.With(
			"match_id", g.MatchID,
			"radiant", g.RadiantTeamName,
			"dire", g.DireTeamName,
			"league_id", g.LeagueID,
		)

		if !isDraftComplete(g) {
			log.Debug("тир-1 матч найден, но драфт не завершён",
				"players_count", len(g.Players),
			)
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

		log.Info("тир-1 матч с завершённым драфтом — запуск анализа",
			"processed_total", len(s.processed),
			"game_number", g.GameNumber,
		)

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

// processMatch runs analysis, sends analytics to Telegram, then watches odds
// for 10 minutes and sends a bet notification with the best odds found.
func (s *Server) processMatch(ctx context.Context, game *steam.LiveLeagueGame) {
	matchID := game.MatchID
	radiantName := game.RadiantTeamName
	direName := game.DireTeamName
	gameNumber := game.GameNumber

	log := slog.With(
		"match_id", matchID,
		"radiant", radiantName,
		"dire", direName,
	)

	log.Info("начало обработки матча")
	matchStart := time.Now()

	// --- Step 1: Run analysis ---
	start := time.Now()
	log.Info("сбор данных для анализа")

	data, err := s.coll.CollectMatchData(ctx, matchID)
	if err != nil {
		log.Error("ошибка сбора данных", "error", err, "duration", time.Since(start).String())
		s.sendError(ctx, matchID, radiantName, direName, fmt.Errorf("сбор данных: %w", err))
		return
	}
	log.Info("данные собраны", "duration", time.Since(start).String())

	llmStart := time.Now()
	log.Info("запуск LLM анализа")

	prediction, err := s.ana.Predict(ctx, data)
	if err != nil {
		log.Error("ошибка LLM анализа", "error", err, "duration", time.Since(llmStart).String())
		s.sendError(ctx, matchID, radiantName, direName, fmt.Errorf("анализ: %w", err))
		return
	}
	log.Info("LLM анализ завершён",
		"duration", time.Since(llmStart).String(),
		"radiant_prob", prediction.Betting.RadiantWinProb,
		"dire_prob", prediction.Betting.DireWinProb,
		"confidence", prediction.Betting.Confidence,
	)

	// Determine predicted winner.
	betTeam := prediction.RadiantTeamName
	if prediction.Betting.DireWinProb > prediction.Betting.RadiantWinProb {
		betTeam = prediction.DireTeamName
	}

	// --- Step 2: Send АНАЛИТИКА message ---
	analyticsMsg := s.buildAnalyticsMessage(prediction, matchID, betTeam)
	if err := s.tgClient.SendMessage(ctx, analyticsMsg); err != nil {
		log.Error("ошибка отправки аналитики в Telegram", "error", err)
	} else {
		log.Info("аналитика отправлена в Telegram",
			"total_duration", time.Since(matchStart).String(),
			"bet_team", betTeam,
		)
	}

	// --- Step 3: Watch odds for 10 minutes, then send bet ---
	bestOdds := s.collectBestOdds(ctx, radiantName, direName, gameNumber, betTeam, matchID)

	bet := betResult{
		hasBet:  true,
		betTeam: betTeam,
	}
	if bestOdds != nil {
		bet.betOdds = matchOddsForTeam(bestOdds, betTeam)
		bet.bookmaker = bestOdds.Bookmaker
	}

	log.Info("ставка",
		"match_id", matchID,
		"bet_team", bet.betTeam,
		"book_odds", bet.betOdds,
		"bookmaker", bet.bookmaker,
	)

	betMsg := s.buildBetMessage(prediction, matchID, bet)
	_ = betMsg // отправка СТАВКА в Telegram отключена
	// if err := s.tgClient.SendMessage(ctx, betMsg); err != nil {
	// 	log.Error("ошибка отправки ставки в Telegram", "error", err)
	// } else {
	// 	log.Info("ставка отправлена в Telegram",
	// 		"total_duration", time.Since(matchStart).String(),
	// 	)
	// }

	// --- Step 4: Record bet to Google Sheets ---
	if s.gsheetsClient != nil {
		winProb := prediction.Betting.RadiantWinProb
		if prediction.Betting.DireWinProb > winProb {
			winProb = prediction.Betting.DireWinProb
		}
		amount := analyzer.KellyBetAmount(winProb, bet.betOdds)
		amount = capBetByConfidence(amount, winProb)
		row := &gsheets.BetRow{
			Date:    time.Now().Format("02.01.2006"),
			Event:   fmt.Sprintf("map %d", gameNumber),
			Team1:   radiantName,
			Team2:   direName,
			BetOn:   bet.betTeam,
			Amount:  amount,
			Odds:    bet.betOdds,
			MatchID: matchID,
			WinProb: winProb,
		}
		if err := s.gsheetsClient.AppendBetRow(ctx, row); err != nil {
			log.Error("ошибка записи ставки в Google Sheets", "error", err)
		} else {
			log.Info("ставка записана в Google Sheets",
				"team", bet.betTeam, "odds", bet.betOdds, "match_id", matchID)
		}
	}
}

// collectBestOdds polls oddspapi every minute for 10 minutes and returns the
// odds snapshot with the best (highest) odds for betTeam. Also fetches pre-match
// odds for the specific map and compares — picks the highest across all sources.
// If no odds are found during the watch period, falls back to the earliest odds
// of a previous map.
func (s *Server) collectBestOdds(ctx context.Context, radiantName, direName string, gameNumber int, betTeam string, matchID int64) *models.MatchOdds {
	log := slog.With("match_id", matchID, "bet_team", betTeam)

	if s.oddsClient == nil {
		log.Debug("OddsPapi не настроен, пропускаю сбор коэффициентов")
		return nil
	}

	log.Info("запуск сбора коэффициентов",
		"watch_duration", oddsWatchDuration.String(),
		"poll_interval", oddsWatchInterval.String(),
	)

	var bestOdds *models.MatchOdds
	var bestPrice float64
	var bestSource string

	// Helper to compare and update best odds.
	tryUpdateWith := func(odds *models.MatchOdds, source string) {
		price := matchOddsForTeam(odds, betTeam)
		if price > bestPrice {
			bestPrice = price
			bestOdds = odds
			bestSource = source
			log.Info("найден лучший коэффициент",
				"price", price,
				"bookmaker", odds.Bookmaker,
				"source", source,
			)
		}
	}

	// Fetch pre-match odds for the specific map (earliest historical line).
	prematchOdds, err := s.oddsClient.FindPreMatchOdds(ctx, radiantName, direName, gameNumber)
	if err != nil {
		log.Debug("прематч коэффициенты недоступны", "error", err)
	} else {
		tryUpdateWith(prematchOdds, "prematch")
	}

	// Poll live odds immediately, then every minute for 10 minutes.
	deadline := time.NewTimer(oddsWatchDuration)
	defer deadline.Stop()

	ticker := time.NewTicker(oddsWatchInterval)
	defer ticker.Stop()

	tryLiveUpdate := func() {
		odds, err := s.oddsClient.FindMatchOddsFresh(ctx, radiantName, direName, gameNumber)
		if err != nil {
			log.Debug("лайв коэффициенты недоступны", "error", err)
			return
		}
		tryUpdateWith(odds, "live")
	}

	// First live poll immediately.
	tryLiveUpdate()

	for {
		select {
		case <-ctx.Done():
			log.Info("сбор коэффициентов отменён", "reason", ctx.Err())
			return bestOdds
		case <-deadline.C:
			log.Info("сбор коэффициентов завершён",
				"best_price", bestPrice,
				"best_source", bestSource,
			)
			// If nothing found, try fallback to previous map.
			if bestOdds == nil && gameNumber > 1 {
				log.Info("пробуем фоллбэк на предыдущую карту")
				for fallback := gameNumber - 1; fallback >= 1; fallback-- {
					odds, err := s.oddsClient.FindMatchOdds(ctx, radiantName, direName, fallback)
					if err == nil {
						log.Info("фоллбэк: коэффициенты предыдущей карты",
							"fallback_map", fallback,
							"bookmaker", odds.Bookmaker,
						)
						return odds
					}
				}
				log.Warn("фоллбэк не удался — коэффициенты не найдены")
			}
			return bestOdds
		case <-ticker.C:
			tryLiveUpdate()
		}
	}
}

// betResult holds the outcome of a betting decision.
type betResult struct {
	hasBet    bool
	betTeam   string
	betOdds   float64
	bookmaker string
}

const separator = "━━━━━━━━━━━━━━━━━━━━━━━━━"

// buildAnalyticsMessage creates the full АНАЛИТИКА Telegram message with
// prediction details, factors, and analysis. Includes predicted winner but no bet.
func (s *Server) buildAnalyticsMessage(pred *models.Prediction, matchID int64, betTeam string) string {
	var sb strings.Builder
	betting := &pred.Betting

	// --- Header ---
	sb.WriteString(fmt.Sprintf("📊 <b>АНАЛИТИКА: %s vs %s</b>\n", pred.RadiantTeamName, pred.DireTeamName))
	sb.WriteString(fmt.Sprintf("Match ID: %d\n\n", matchID))
	sb.WriteString(fmt.Sprintf("🏆 Победитель: <b>%s</b>\n", betTeam))

	// --- Probabilities ---
	sb.WriteString("\n")
	sb.WriteString(separator)
	sb.WriteString("\n\n")

	if betting.RadiantWinProb > 0 {
		sb.WriteString("📊 <b>Шанс на победу:</b>\n")
		sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.RadiantTeamName, betting.RadiantWinProb))
		sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.DireTeamName, betting.DireWinProb))
		if betting.Confidence != "" {
			sb.WriteString(fmt.Sprintf("  Уверенность: %s\n", strings.ToUpper(betting.Confidence)))
		}
	}

	if betting.DraftRadiantProb > 0 {
		sb.WriteString(fmt.Sprintf("\n🎲 <b>Драфт:</b>\n"))
		sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.RadiantTeamName, betting.DraftRadiantProb))
		sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.DireTeamName, betting.DraftDireProb))
	}

	// --- Factors ---
	if len(pred.Factors) > 0 {
		sb.WriteString("\n")
		sb.WriteString(separator)
		sb.WriteString("\n\n")
		sb.WriteString("⚖️ <b>Оценка по факторам:</b>\n")
		for i, f := range pred.Factors {
			emoji := factorEmoji(i)
			team := advantageLabel(f.Advantage, pred.RadiantTeamName, pred.DireTeamName)
			if strings.EqualFold(f.Advantage, "Equal") {
				sb.WriteString(fmt.Sprintf("  %s %s → %s\n", emoji, f.Name, team))
			} else {
				sb.WriteString(fmt.Sprintf("  %s %s → %s, %s преимущество\n", emoji, f.Name, team, f.Degree))
			}
		}
	}

	// --- Analysis ---
	sb.WriteString("\n")
	sb.WriteString(separator)
	sb.WriteString("\n\n")
	sb.WriteString("📝 <b>Анализ:</b>\n")
	sb.WriteString(telegram.MDToTelegramHTML(pred.Analysis))

	return sb.String()
}

// buildBetMessage creates a compact СТАВКА Telegram message with the team and odds.
func (s *Server) buildBetMessage(pred *models.Prediction, matchID int64, bet betResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🎯 <b>СТАВКА: %s vs %s</b>\n", pred.RadiantTeamName, pred.DireTeamName))
	sb.WriteString(fmt.Sprintf("Match ID: %d\n\n", matchID))
	sb.WriteString(fmt.Sprintf("💰 Ставка на: <b>%s</b>\n", bet.betTeam))
	if bet.betOdds > 0 {
		sb.WriteString(fmt.Sprintf("📌 Коэффициент: <b>%.2f</b>\n", bet.betOdds))
		if bet.bookmaker != "" {
			sb.WriteString(fmt.Sprintf("📎 Букмекер: %s\n", bet.bookmaker))
		}
	}

	sb.WriteString(fmt.Sprintf("\n📊 <b>Шанс на победу:</b>\n"))
	sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.RadiantTeamName, pred.Betting.RadiantWinProb))
	sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", pred.DireTeamName, pred.Betting.DireWinProb))
	if pred.Betting.Confidence != "" {
		sb.WriteString(fmt.Sprintf("  Уверенность: %s\n", strings.ToUpper(pred.Betting.Confidence)))
	}

	return sb.String()
}

// factorEmoji returns an emoji for a factor by its index (0-based).
func factorEmoji(index int) string {
	emojis := []string{"🏆", "⚔️", "🛡", "🌍", "🎮", "📋", "🤝"}
	if index < len(emojis) {
		return emojis[index]
	}
	return "•"
}

// advantageLabel maps "Radiant"/"Dire"/"Equal" to the actual team name.
func advantageLabel(advantage, radiantName, direName string) string {
	switch advantage {
	case "Radiant":
		return radiantName
	case "Dire":
		return direName
	default:
		return "Equal"
	}
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

const (
	oddsWatchDuration = 10 * time.Minute
	oddsWatchInterval = 1 * time.Minute
)

// capBetByConfidence ограничивает размер ставки при низкой уверенности.
// winProb: 0-100.
func capBetByConfidence(amount int, winProb float64) int {
	var maxAmount int
	switch {
	case winProb < 60:
		maxAmount = 2000
	case winProb < 65:
		maxAmount = 3000
	case winProb < 70:
		maxAmount = 4000
	default:
		return amount
	}
	if amount > maxAmount {
		return maxAmount
	}
	return amount
}


func (s *Server) sendError(ctx context.Context, matchID int64, radiant, dire string, err error) {
	msg := fmt.Sprintf("<b>ОШИБКА: %s vs %s</b>\nMatch ID: %d\n\n%v", radiant, dire, matchID, err)
	if sendErr := s.tgClient.SendMessage(ctx, msg); sendErr != nil {
		slog.Error("не удалось отправить ошибку в Telegram",
			"match_id", matchID,
			"original_error", err,
			"send_error", sendErr,
		)
	}
}

// startResultChecker launches a background goroutine that checks pending bet results every hour.
func (s *Server) startResultChecker(ctx context.Context) {
	if s.gsheetsClient == nil {
		return
	}
	slog.Info("запуск проверки результатов ставок", "interval", "1h")

	go func() {
		// Initial check after 2 minutes (let the server warm up).
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
			s.checkResults(ctx)
		}

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkResults(ctx)
			}
		}
	}()
}

// checkResults reads pending bets from Google Sheets and fills in results for finished matches.
func (s *Server) checkResults(ctx context.Context) {
	log := slog.With("component", "result_checker")
	log.Info("проверка незавершённых ставок")

	pending, err := s.gsheetsClient.GetPendingRows(ctx)
	if err != nil {
		log.Error("ошибка чтения Google Sheets", "error", err)
		return
	}

	if len(pending) == 0 {
		log.Info("нет незавершённых ставок")
		return
	}
	log.Info("найдено незавершённых ставок", "count", len(pending))

	for _, row := range pending {
		match, err := s.odClient.GetMatch(ctx, row.MatchID)
		if err != nil {
			log.Debug("матч ещё не доступен", "match_id", row.MatchID, "error", err)
			continue
		}

		// Duration == 0 means OpenDota hasn't fully parsed the match yet;
		// radiant_win would default to false and produce wrong results.
		if match.Duration == 0 {
			log.Debug("матч ещё не распарсен", "match_id", row.MatchID)
			continue
		}

		// Determine the winning team name.
		// Team1 in the sheet is always the radiant team.
		var winner string
		if match.RadiantWin {
			winner = row.Team1
		} else {
			winner = row.Team2
		}

		result := "L"
		if fuzzyContains(strings.ToLower(winner), strings.ToLower(row.BetTeam)) {
			result = "W"
		}

		if err := s.gsheetsClient.WriteResult(ctx, row.RowNumber, result); err != nil {
			log.Error("ошибка записи результата", "match_id", row.MatchID, "row", row.RowNumber, "error", err)
			continue
		}

		log.Info("результат записан",
			"match_id", row.MatchID,
			"result", result,
			"winner", winner,
			"bet_team", row.BetTeam,
		)
	}
}

func tier1TeamNames() []string {
	names := make([]string, 0, len(tier1.Teams))
	for _, name := range tier1.Teams {
		names = append(names, name)
	}
	return names
}
