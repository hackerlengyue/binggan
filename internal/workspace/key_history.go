package workspace

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"time.haomen/binggan/v2/internal/store"
)

const keyHistorySetting = "key_history_v1"

var errInvalidKeyHistory = errors.New("密钥历史格式无效")

type keyHistoryDocument struct {
	Version           int                `json:"version"`
	Items             []json.RawMessage  `json:"items"`
	CurrentCapture    *keyHistorySession `json:"currentCapture"`
	DeletedCaptureIDs []string           `json:"deletedCaptureIds"`
	RetiredCaptureIDs []string           `json:"retiredCaptureIds"`
}
type keyHistorySession struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	StoppedAt string `json:"stoppedAt,omitempty"`
}
type keyHistoryMetadata struct {
	ID        string          `json:"id"`
	UpdatedAt string          `json:"updatedAt"`
	CreatedAt string          `json:"createdAt"`
	KeyJSON   json.RawMessage `json:"keyJson"`
}

func emptyKeyHistory() keyHistoryDocument {
	return keyHistoryDocument{Version: 1, Items: []json.RawMessage{}, DeletedCaptureIDs: []string{}, RetiredCaptureIDs: []string{}}
}
func validHistoryTime(v string) bool { _, err := time.Parse(time.RFC3339Nano, v); return err == nil }
func (d keyHistoryDocument) valid() bool {
	if d.Version != 1 || d.Items == nil || len(d.Items) > 10000 || len(d.DeletedCaptureIDs) > 20000 || len(d.RetiredCaptureIDs) > 20000 {
		return false
	}
	ids := map[string]bool{}
	for _, raw := range d.Items {
		var m keyHistoryMetadata
		if json.Unmarshal(raw, &m) != nil || m.ID == "" || len(m.ID) > 200 || ids[m.ID] || !validHistoryTime(m.UpdatedAt) || !validHistoryTime(m.CreatedAt) || len(m.KeyJSON) == 0 || string(m.KeyJSON) == "null" {
			return false
		}
		ids[m.ID] = true
	}
	for _, id := range d.DeletedCaptureIDs {
		if id == "" || len(id) > 200 {
			return false
		}
	}
	for _, id := range d.RetiredCaptureIDs {
		if id == "" || len(id) > 200 {
			return false
		}
	}
	s := d.CurrentCapture
	return s == nil || (s.ID != "" && validHistoryTime(s.CreatedAt) && (s.StoppedAt == "" || validHistoryTime(s.StoppedAt)))
}

// KeyHistory returns the durable document without changing its JSON schema.
func (a *App) KeyHistory() (string, error) {
	d := emptyKeyHistory()
	if err := a.Store.GetSetting(keyHistorySetting, &d); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	raw, err := json.Marshal(d)
	return string(raw), err
}

// SaveKeyHistory validates and merges a complete snapshot in one transaction.
// A stale WebView cannot resurrect entries that another window deleted.
func (a *App) SaveKeyHistory(raw string) (string, error) {
	if len(raw) > 32<<20 {
		return "", errInvalidKeyHistory
	}
	var incoming keyHistoryDocument
	if json.Unmarshal([]byte(raw), &incoming) != nil || !incoming.valid() {
		return "", errInvalidKeyHistory
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	current := emptyKeyHistory()
	var saved string
	err = tx.QueryRow("SELECT value FROM settings WHERE key=?", keyHistorySetting).Scan(&saved)
	if err == nil {
		err = json.Unmarshal([]byte(saved), &current)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	deleted := map[string]bool{}
	retired := map[string]bool{}
	for _, d := range []keyHistoryDocument{current, incoming} {
		for _, id := range d.DeletedCaptureIDs {
			deleted[id] = true
		}
		for _, id := range d.RetiredCaptureIDs {
			retired[id] = true
		}
	}
	if len(deleted) > 20000 || len(retired) > 20000 {
		return "", errInvalidKeyHistory
	}
	items := map[string]json.RawMessage{}
	dates := map[string]time.Time{}
	for _, d := range []keyHistoryDocument{current, incoming} {
		for _, item := range d.Items {
			var m keyHistoryMetadata
			_ = json.Unmarshal(item, &m)
			at, _ := time.Parse(time.RFC3339Nano, m.UpdatedAt)
			if !deleted[m.ID] && !at.Before(dates[m.ID]) {
				items[m.ID] = item
				dates[m.ID] = at
			}
		}
	}
	result := emptyKeyHistory()
	for id := range deleted {
		result.DeletedCaptureIDs = append(result.DeletedCaptureIDs, id)
	}
	sort.Strings(result.DeletedCaptureIDs)
	for id := range retired {
		result.RetiredCaptureIDs = append(result.RetiredCaptureIDs, id)
	}
	sort.Strings(result.RetiredCaptureIDs)
	for _, item := range items {
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		var a, b keyHistoryMetadata
		_ = json.Unmarshal(result.Items[i], &a)
		_ = json.Unmarshal(result.Items[j], &b)
		return dates[a.ID].After(dates[b.ID])
	})
	if current.CurrentCapture != nil && !retired[current.CurrentCapture.ID] {
		result.CurrentCapture = current.CurrentCapture
	}
	if s := incoming.CurrentCapture; s != nil && !retired[s.ID] {
		old := result.CurrentCapture
		at, _ := time.Parse(time.RFC3339Nano, s.CreatedAt)
		var oldAt time.Time
		if old != nil {
			oldAt, _ = time.Parse(time.RFC3339Nano, old.CreatedAt)
		}
		if old == nil || at.After(oldAt) || (old.ID == s.ID && (old.StoppedAt == "" || s.StoppedAt != "")) {
			result.CurrentCapture = s
		}
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", keyHistorySetting, store.Encode(result)); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return "", err
	}
	output, err := json.Marshal(result)
	return string(output), err
}
