package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// InsertQueuedLogs atomically rejects delayed browser writes and historical
// native records at or before the user's last clear operation.
func (s *Store) InsertQueuedLogs(entries []Log) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoffs := map[string]time.Time{}
	for _, key := range []string{"logs_cleared_at", "connection_logs_cleared_at"} {
		var raw string
		err = tx.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		var cutoff time.Time
		if err = json.Unmarshal([]byte(raw), &cutoff); err != nil {
			return err
		}
		cutoffs[key] = cutoff
	}
	for _, l := range entries {
		at, err := time.Parse(time.RFC3339Nano, l.At)
		if err != nil {
			return err
		}
		if !at.After(cutoffs["logs_cleared_at"]) || (l.Source == "connection" && !at.After(cutoffs["connection_logs_cleared_at"])) {
			continue
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO system_logs VALUES(?,?,?,?,?,?,?)", l.ID, at.UTC().Format(time.RFC3339Nano), l.Level, l.Source, l.Message, l.Detail, l.RequestID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
