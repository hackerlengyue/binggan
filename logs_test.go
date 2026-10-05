package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/workspace"
)

func TestUnifiedLogsImportDeduplicateClearAndResume(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := workspace.New(config.Config{Root: root, DataDir: root, Host: "127.0.0.1", Port: 18768}, db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dir := filepath.Join(root, "capture")
	legacy := capture.NewLogbook(dir)
	legacy.Add("warn", "historical native warning", "legacy detail")
	legacy.AddCertificate("info", "historical certificate", "certificate detail")
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	oldFile, err := os.ReadFile(filepath.Join(dir, "logs", "capture.log"))
	if err != nil {
		t.Fatal(err)
	}
	logs := capture.NewLogbook(dir)
	defer logs.Close()
	sink := captureLogSink(db)
	if err := logs.Connect(sink); err != nil {
		t.Fatal(err)
	}
	// Replaying the same retained file after a restart must not duplicate entries.
	if err := logs.Connect(sink); err != nil {
		t.Fatal(err)
	}
	logs.AddStatus("info", "live native status")
	logs.AddBackend("warn", "already persisted backend warning", "")
	if err := db.Log("warn", "capture", "already persisted backend warning", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Log("info", "server", "service entry", "", ""); err != nil {
		t.Fatal(err)
	}
	get := func() []store.Log {
		result, err := a.Logs(workspace.LogQuery{Page: 1, PageSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		return result.Items
	}
	if items := get(); len(items) != 5 {
		t.Fatalf("unified records missing or duplicated: %+v", items)
	}
	a.SetLogClearer((&Monitor{logs: logs}).clearLogbook)
	if _, err := a.ClearLogs("all"); err != nil || len(get()) != 0 || len(logs.Entries()) != 0 {
		t.Fatal("clear incomplete", err)
	}
	// Simulate an old archive copied back after clearing. The durable cutoff must
	// reject it even though the cleared file and SQLite use different storage.
	if err := os.WriteFile(filepath.Join(dir, "logs", "capture-2026-09-30T01-00-00.000.log"), oldFile, 0600); err != nil {
		t.Fatal(err)
	}
	if err := logs.Connect(sink); err != nil {
		t.Fatal(err)
	}
	if len(get()) != 0 {
		t.Fatal("cleared history returned after replay")
	}
	logs.AddCertificate("info", "new certificate entry", "new detail")
	items := get()
	if len(items) != 1 || items[0].Source != "certificate" || !strings.Contains(items[0].Message, "new certificate") {
		t.Fatal(items)
	}
}
