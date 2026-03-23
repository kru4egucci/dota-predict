package gc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/paralin/go-dota2"
	"github.com/paralin/go-dota2/protocol"
	"github.com/paralin/go-steam"
	"github.com/sirupsen/logrus"
)

var (
	ErrNotConnected = errors.New("GC: не подключен")
	ErrMatchNotFound = errors.New("GC: матч не найден")
)

// Config holds Steam account credentials for GC connection.
type Config struct {
	Username   string
	Password   string
	AuthCode   string // Steam Guard code (first login only)
	SentryFile string // Path to sentry hash file for subsequent logins
}

// DraftPlayer holds a player's hero pick from GC data.
type DraftPlayer struct {
	AccountID uint32
	HeroID    int32
	Team      uint32 // 0=radiant, 1=dire
}

// Client connects to the Dota 2 Game Coordinator via Steam to get real-time
// draft data that may not yet be available through the Steam Web API.
type Client struct {
	cfg Config

	mu   sync.RWMutex
	dota *dota2.Dota2 // nil until GC session attempt
}

// NewClient creates a new GC client. Does not connect — call Run() to start.
func NewClient(cfg Config) *Client {
	if cfg.SentryFile == "" {
		cfg.SentryFile = "steam_sentry.bin"
	}
	return &Client{cfg: cfg}
}

// Run starts the Steam connection loop. Blocks until ctx is cancelled.
// Automatically reconnects on disconnect with exponential backoff.
func (c *Client) Run(ctx context.Context) {
	backoff := 5 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		err := c.runSession(ctx)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			slog.Warn("GC: сессия завершилась с ошибкой", "error", err, "reconnect_in", backoff.String())
		} else {
			slog.Info("GC: сессия завершилась, переподключение", "reconnect_in", backoff.String())
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Increase backoff up to 60s.
		if backoff < 60*time.Second {
			backoff = backoff * 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		}
	}
}

// runSession establishes a single Steam+GC session and processes events until
// disconnect or context cancellation. Returns nil on clean disconnect.
func (c *Client) runSession(ctx context.Context) error {
	sc := steam.NewClient()
	sc.ConnectionTimeout = 30 * time.Second

	slog.Info("GC: подключение к Steam")
	sc.Connect()

	// Load sentry hash for machine auth.
	var sentryHash steam.SentryHash
	if data, err := os.ReadFile(c.cfg.SentryFile); err == nil {
		sentryHash = data
		slog.Debug("GC: sentry файл загружен", "path", c.cfg.SentryFile)
	}

	var d *dota2.Dota2
	defer func() {
		c.mu.Lock()
		c.dota = nil
		c.mu.Unlock()
		if d != nil {
			d.SetPlaying(false)
			d.Close()
		}
		sc.Disconnect()
	}()

	lgr := logrus.New()
	lgr.SetLevel(logrus.WarnLevel)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-sc.Events():
			switch e := event.(type) {
			case *steam.ConnectedEvent:
				slog.Info("GC: подключено к Steam, авторизация")
				details := &steam.LogOnDetails{
					Username:       c.cfg.Username,
					Password:       c.cfg.Password,
					SentryFileHash: sentryHash,
				}
				if c.cfg.AuthCode != "" {
					details.AuthCode = c.cfg.AuthCode
				}
				sc.Auth.LogOn(details)

			case *steam.LoggedOnEvent:
				slog.Info("GC: авторизация успешна", "steam_id", e.ClientSteamId)

				d = dota2.New(sc, lgr)
				d.SetPlaying(true)
				d.SayHello()

				c.mu.Lock()
				c.dota = d
				c.mu.Unlock()

				slog.Info("GC: Dota 2 GC hello отправлен, ожидание сессии")

			case *steam.LogOnFailedEvent:
				return fmt.Errorf("авторизация неуспешна: %v", e.Result)

			case *steam.MachineAuthUpdateEvent:
				slog.Info("GC: обновление sentry файла", "path", c.cfg.SentryFile)
				sentryHash = e.Hash
				if err := os.WriteFile(c.cfg.SentryFile, e.Hash, 0600); err != nil {
					slog.Error("GC: ошибка сохранения sentry файла", "error", err)
				}

			case *steam.DisconnectedEvent:
				slog.Warn("GC: отключено от Steam")
				return nil

			case steam.FatalErrorEvent:
				return fmt.Errorf("фатальная ошибка Steam: %v", e)
			}
		}
	}
}

// GetLiveMatchDraft queries the Game Coordinator for hero picks in a live match.
// Uses lobby ID for precise lookup. Returns ErrNotConnected if GC session is not
// established, ErrMatchNotFound if the match is not found in GC response.
func (c *Client) GetLiveMatchDraft(ctx context.Context, matchID, lobbyID int64) ([]DraftPlayer, error) {
	c.mu.RLock()
	d := c.dota
	c.mu.RUnlock()

	if d == nil {
		return nil, ErrNotConnected
	}

	// Query by lobby ID for precise match lookup.
	req := &protocol.CMsgClientToGCFindTopSourceTVGames{
		LobbyIds: []uint64{uint64(lobbyID)},
	}

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := d.FindTopSourceTVGames(queryCtx, req)
	if err != nil {
		return nil, fmt.Errorf("FindTopSourceTVGames: %w", err)
	}

	// Search for our match in the response.
	for _, game := range resp.GetGameList() {
		if game.GetMatchId() == uint64(matchID) || game.GetLobbyId() == uint64(lobbyID) {
			return extractPlayers(game), nil
		}
	}

	return nil, ErrMatchNotFound
}

// IsConnected returns true if the GC client has an active session attempt.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dota != nil
}

func extractPlayers(game *protocol.CSourceTVGameSmall) []DraftPlayer {
	raw := game.GetPlayers()
	players := make([]DraftPlayer, 0, len(raw))
	for _, p := range raw {
		players = append(players, DraftPlayer{
			AccountID: p.GetAccountId(),
			HeroID:    p.GetHeroId(),
			Team:      p.GetTeam(),
		})
	}
	return players
}
