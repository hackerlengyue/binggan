package workspace

import (
	"encoding/json"
	"testing"

	"time.haomen/binggan/v2/internal/store"
)

func TestKeyHistorySurvivesReopenAndStaleBrowser(t *testing.T) {
	a := testApp(t)
	item := func(id, at string) map[string]any {
		return map[string]any{"id": id, "createdAt": at, "updatedAt": at, "keyJson": map[string]any{"passwords": map[string]string{}, "getPwdData": map[string]any{}}}
	}
	doc := func(items []map[string]any, deleted []string) string {
		return store.Encode(map[string]any{"version": 1, "items": items, "deletedCaptureIds": deleted})
	}
	one := item("first", "2026-09-26T01:00:00Z")
	two := item("second", "2026-09-26T02:00:00Z")
	if _, err := a.SaveKeyHistory(doc([]map[string]any{one}, nil)); err != nil {
		t.Fatal(err)
	}
	// Reopen SQLite to exercise persistence independently of the WebView.
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(a.Config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	a.Store = reopened
	t.Cleanup(func() { _ = reopened.Close() })
	raw, err := a.KeyHistory()
	if err != nil {
		t.Fatal(err)
	}
	var got keyHistoryDocument
	if err := json.Unmarshal([]byte(raw), &got); err != nil || len(got.Items) != 1 {
		t.Fatalf("history after reopen = %s, %v", raw, err)
	}
	for _, snapshot := range []string{
		doc([]map[string]any{two}, nil),
		doc([]map[string]any{}, []string{"first"}),
		doc([]map[string]any{one}, nil),
	} {
		if _, err := a.SaveKeyHistory(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	raw, err = a.KeyHistory()
	if err != nil || json.Unmarshal([]byte(raw), &got) != nil || len(got.Items) != 1 {
		t.Fatalf("stale snapshot changed history: %s, %v", raw, err)
	}
	var survivor keyHistoryMetadata
	if err := json.Unmarshal(got.Items[0], &survivor); err != nil || survivor.ID != "second" {
		t.Fatalf("survivor = %+v, %v", survivor, err)
	}
	if _, err := a.SaveKeyHistory(`{"version":9}`); err == nil {
		t.Fatal("invalid history accepted")
	}
}

func TestRetiredCaptureDoesNotReviveFromStaleSnapshot(t *testing.T) {
	a := testApp(t)
	at := "2026-09-26T01:00:00Z"
	item := map[string]any{"id": "capture-a", "createdAt": at, "updatedAt": at, "keyJson": map[string]any{"passwords": map[string]string{}, "getPwdData": map[string]any{}}}
	session := map[string]any{"id": "capture-a", "createdAt": at}
	stale := store.Encode(map[string]any{"version": 1, "items": []any{item}, "currentCapture": session})
	if _, err := a.SaveKeyHistory(stale); err != nil {
		t.Fatal(err)
	}
	clear := store.Encode(map[string]any{"version": 1, "items": []any{item}, "currentCapture": nil, "retiredCaptureIds": []string{"capture-a"}})
	if _, err := a.SaveKeyHistory(clear); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveKeyHistory(stale); err != nil {
		t.Fatal(err)
	}
	raw, err := a.KeyHistory()
	if err != nil {
		t.Fatal(err)
	}
	var saved keyHistoryDocument
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CurrentCapture != nil || len(saved.RetiredCaptureIDs) != 1 || saved.RetiredCaptureIDs[0] != "capture-a" || len(saved.Items) != 1 {
		t.Fatalf("clear did not preserve the entry or retire only the session: %s", raw)
	}
	newSession := store.Encode(map[string]any{"version": 1, "items": []any{item}, "currentCapture": map[string]any{"id": "capture-b", "createdAt": "2026-09-26T02:00:00Z"}})
	if _, err := a.SaveKeyHistory(newSession); err != nil {
		t.Fatal(err)
	}
	raw, err = a.KeyHistory()
	if err != nil || json.Unmarshal([]byte(raw), &saved) != nil || saved.CurrentCapture == nil || saved.CurrentCapture.ID != "capture-b" {
		t.Fatalf("new capture was not accepted after retirement: %s, %v", raw, err)
	}
}
