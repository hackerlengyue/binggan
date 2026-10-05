package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEntriesReusesSnapshotUntilMutation(t *testing.T) {
	l := NewLogbook(t.TempDir())
	defer l.Close()
	l.Add("info", "one", "detail")
	first := l.Entries()
	second := l.Entries()
	if len(first) != 1 || &first[0] != &second[0] {
		t.Fatal("unchanged log was copied again")
	}
	l.Add("info", "two", "")
	third := l.Entries()
	if len(third) != 2 || &third[0] == &first[0] {
		t.Fatal("log write reused a stale snapshot")
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(l.Entries()) != 0 {
		t.Fatal("clear left the snapshot in place")
	}
	l.Add("info", "after", "")
	if items := l.Entries(); len(items) != 1 || items[0].Message != "after" {
		t.Fatalf("logging did not resume after clear: %+v", items)
	}
}

func TestClearLogsRemovesRecordsAndBackupsThenContinuesWriting(t *testing.T) {
	dir := t.TempDir()
	l := NewLogbook(dir)
	defer l.Close()
	l.Add("info", "before rotation", "")
	if err := l.writer.Rotate(); err != nil {
		t.Fatal(err)
	}
	l.Add("warn", "before clear", "old detail")
	lastID := l.Entries()[1].ID
	unrelated := filepath.Join(dir, "logs", "capture-not-a-backup.log")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(l.Entries()) != 0 {
		t.Fatal("visible logs were not cleared")
	}
	data, err := os.ReadFile(l.writer.Filename)
	if err != nil || len(data) != 0 {
		t.Fatalf("exported file still contains old logs: %q, %v", data, err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if isLogBackup(f.Name()) {
			t.Fatal("rotated log survived clear:", f.Name())
		}
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file changed")
	}
	l.Add("info", "after clear", "new detail")
	items := l.Entries()
	if len(items) != 1 || items[0].ID <= lastID || items[0].Message != "after clear" {
		t.Fatalf("logging did not resume correctly: %+v", items)
	}
	data, err = os.ReadFile(l.writer.Filename)
	if err != nil || !strings.Contains(string(data), "after clear") || strings.Contains(string(data), "before clear") {
		t.Fatalf("new file contents incorrect: %q, %v", data, err)
	}
}

func TestClearLogsSerializesWithLiveWrites(t *testing.T) {
	l := NewLogbook(t.TempDir())
	defer l.Close()
	var workers sync.WaitGroup
	for i := 0; i < 6; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 30; j++ {
				l.Add("info", "live event", "")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		if err := l.Clear(); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	l.Add("info", "last event", "")
	data, err := os.ReadFile(l.writer.Filename)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	items := l.Entries()
	if len(lines) != len(items) {
		t.Fatalf("file and window diverged: %d vs %d", len(lines), len(items))
	}
	for i, line := range lines {
		var record struct {
			Message string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil || record.Message != items[i].Message {
			t.Fatalf("invalid live log after clear: %q, %v", line, err)
		}
	}
}

func TestClearClosedLogbookDoesNotReopenFile(t *testing.T) {
	l := NewLogbook(t.TempDir())
	l.Add("info", "preserve", "")
	l.Close()
	if err := l.Clear(); err == nil {
		t.Fatal("closed logbook was cleared")
	}
	if data, err := os.ReadFile(l.writer.Filename); err != nil || !strings.Contains(string(data), "preserve") {
		t.Fatalf("closed file changed: %q, %v", data, err)
	}
}

func TestCertificateProgressBypassesClosedRuntimeGate(t *testing.T) {
	l := NewLogbook(t.TempDir())
	defer l.Close()
	l.SetRuntimeEnabled(false)
	l.Add("warn", "suppressed network failure", "")
	l.AddCertificate("info", "waiting for system authorization", "keychain")
	entries := l.Entries()
	if len(entries) != 1 || entries[0].Scope != "certificate" {
		t.Fatalf("installation logs missing or runtime leaked: %+v", entries)
	}
	data, err := os.ReadFile(l.writer.Filename)
	if err != nil || strings.Contains(string(data), "suppressed network") || !strings.Contains(string(data), "system authorization") {
		t.Fatalf("incorrect persisted logs: %s %v", data, err)
	}
	l.SetRuntimeEnabled(true)
	l.Add("info", "runtime started", "")
	if len(l.Entries()) != 2 {
		t.Fatal("runtime gate did not reopen")
	}
}
