package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/store"
)

func captureLogSink(db *store.Store) func([]capture.Entry) error {
	return func(entries []capture.Entry) error {
		logs := make([]store.Log, 0, len(entries))
		for _, entry := range entries {
			if entry.Scope == "backend" {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, entry.Time)
			if err != nil {
				return err
			}
			level := strings.ToLower(entry.Level)
			if level != "warn" && level != "error" {
				level = "info"
			}
			source := "capture"
			if entry.Scope == "certificate" {
				source = "certificate"
			}
			// File replay and live delivery use the same stable identity. The in-memory
			// sequence starts again on restart, so it cannot identify persisted logs.
			identity, _ := json.Marshal([]string{at.UTC().Format(time.RFC3339Nano), level, source, entry.Message, entry.Detail})
			logs = append(logs, store.Log{ID: fmt.Sprintf("capture:%x", sha256.Sum256(identity)), At: at.UTC().Format(time.RFC3339Nano), Level: level, Source: source, Message: entry.Message, Detail: entry.Detail})
		}
		return db.InsertQueuedLogs(logs)
	}
}

// Clear only the logbook lock; certificate/TUN operations can keep running.
// The logbook serializes this with live writes and shutdown on its own.
func (a *Monitor) clearLogbook() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.logs == nil {
		return nil
	}
	return a.logs.Clear()
}
