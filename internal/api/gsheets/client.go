package gsheets

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Client wraps Google Sheets API v4 for bet tracking.
type Client struct {
	service       *sheets.SpreadsheetsValuesService
	spreadsheetID string
	sheetName     string
}

// BetRow represents a single bet entry to be written to the sheet.
type BetRow struct {
	Date    string  // "02.01.2006" format
	Event   string  // "map 1", "map 2", "map 3"
	Team1   string  // radiant team name
	Team2   string  // dire team name
	BetOn   string  // team name the bet is placed on
	Amount  int     // bet amount (always 1000)
	Odds    float64 // bookmaker odds
	MatchID int64   // for result lookup
}

// PendingRow represents a bet row without a result yet.
type PendingRow struct {
	RowNumber int    // 1-based sheet row number
	MatchID   int64  // match ID for API lookup
	BetTeam   string // column E (Ставка на)
	Team1     string // column C (Команда 1)
	Team2     string // column D (Команда 2)
}

// NewClient creates a Google Sheets client using a service account credentials file.
// Returns nil if credentialsFile is empty (feature is disabled).
func NewClient(credentialsFile, spreadsheetID, sheetName string) (*Client, error) {
	if credentialsFile == "" {
		return nil, nil
	}

	ctx := context.Background()
	srv, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("создание Google Sheets сервиса: %w", err)
	}

	return &Client{
		service:       srv.Spreadsheets.Values,
		spreadsheetID: spreadsheetID,
		sheetName:     sheetName,
	}, nil
}

// AppendBetRow writes a new bet row to the first empty row in the sheet.
func (c *Client) AppendBetRow(ctx context.Context, row *BetRow) error {
	profitFormula := `=IF(INDIRECT("H"&ROW())="W",INDIRECT("F"&ROW())*INDIRECT("G"&ROW())-INDIRECT("F"&ROW()),IF(INDIRECT("H"&ROW())="L",-INDIRECT("F"&ROW()),""))`

	values := []interface{}{
		row.Date,
		row.Event,
		row.Team1,
		row.Team2,
		row.BetOn,
		row.Amount,
		row.Odds,
		"",             // Результат — empty initially
		profitFormula,  // Профит — auto-calculated
		row.MatchID,    // Column J — match ID for result checking
	}

	rangeStr := fmt.Sprintf("'%s'!A:J", c.sheetName)
	vr := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err := c.service.Append(c.spreadsheetID, rangeStr, vr).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("запись строки в Google Sheets: %w", err)
	}

	return nil
}

// GetPendingRows returns all rows that have a match ID but no result yet.
func (c *Client) GetPendingRows(ctx context.Context) ([]PendingRow, error) {
	rangeStr := fmt.Sprintf("'%s'!A:J", c.sheetName)
	resp, err := c.service.Get(c.spreadsheetID, rangeStr).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("чтение Google Sheets: %w", err)
	}

	var pending []PendingRow
	for i, row := range resp.Values {
		if i == 0 {
			continue // skip header
		}

		result := cellString(row, 7)    // column H (Результат)
		matchIDStr := cellString(row, 9) // column J (MatchID)

		if result != "" || matchIDStr == "" {
			continue
		}

		matchID, err := strconv.ParseInt(matchIDStr, 10, 64)
		if err != nil {
			slog.Debug("не удалось распарсить match_id в строке", "row", i+1, "value", matchIDStr)
			continue
		}

		pending = append(pending, PendingRow{
			RowNumber: i + 1, // 1-based
			MatchID:   matchID,
			BetTeam:   cellString(row, 4), // column E (Ставка на)
			Team1:     cellString(row, 2), // column C (Команда 1)
			Team2:     cellString(row, 3), // column D (Команда 2)
		})
	}

	return pending, nil
}

// WriteResult writes "W" or "L" into the Результат column for the given row.
func (c *Client) WriteResult(ctx context.Context, rowNumber int, result string) error {
	cell := fmt.Sprintf("'%s'!H%d", c.sheetName, rowNumber)
	vr := &sheets.ValueRange{
		Values: [][]interface{}{{result}},
	}

	_, err := c.service.Update(c.spreadsheetID, cell, vr).
		ValueInputOption("RAW").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("запись результата в строку %d: %w", rowNumber, err)
	}

	return nil
}

// cellString safely extracts a string from a row at the given 0-based index.
func cellString(row []interface{}, index int) string {
	if index >= len(row) {
		return ""
	}
	s, _ := row[index].(string)
	if s == "" {
		// Handle numeric values from Sheets API (e.g., match IDs come as float64).
		switch v := row[index].(type) {
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case int64:
			return strconv.FormatInt(v, 10)
		}
	}
	return s
}
