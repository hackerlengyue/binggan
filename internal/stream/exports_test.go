package stream

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/tasks"
	"time.haomen/binggan/v2/internal/workspace"
)

const (
	exportTaskID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	exportJob1   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	exportJob2   = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	exportJob3   = "dddddddd-dddd-dddd-dddd-dddddddddddd"
)

func exportFixture(t *testing.T) (*Handler, *workspace.App, string) {
	t.Helper()
	root := t.TempDir()
	if err := workspace.EnsureDirectories(root); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	task := tasks.Task{ID: exportTaskID, Name: "课程/练习", JobIDs: []string{exportJob1, exportJob2, exportJob3}}
	if _, err := db.DB.Exec("INSERT INTO tasks VALUES(?,?)", task.ID, store.Encode(task)); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id, status, payload string
	}{
		{exportJob1, "completed", "video-one"},
		{exportJob2, "completed", "video-two"},
		{exportJob3, "failed", ""},
	} {
		job := tasks.Job{ID: item.id, TaskID: task.ID, Name: "same.sz", Status: item.status}
		if _, err := db.DB.Exec("INSERT INTO jobs VALUES(?,?,?)", item.id, task.ID, store.Encode(job)); err != nil {
			t.Fatal(err)
		}
		if item.payload != "" {
			dir := filepath.Join(dataDir, "decrypt", item.id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "output.mp4"), []byte(item.payload), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for at, chunk := range [][]byte{[]byte("first\n"), []byte("second\n")} {
		if _, err := db.DB.Exec("INSERT INTO job_logs VALUES(?,?,?)", exportJob1, at*6, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InsertLog(store.Log{ID: "server-log", At: store.Now(), Level: "warn", Source: "server", Message: "server issue", Detail: "detail"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertLog(store.Log{ID: "capture-log", At: store.Now(), Level: "info", Source: "capture", Message: "capture issue"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO captures(payload) VALUES(?)", `{"id":"capture-one","phase":"request","method":"GET","url":"https://example.com","host":"example.com","headers":{},"body":""}`); err != nil {
		t.Fatal(err)
	}
	app, err := workspace.New(config.Config{Root: root, DataDir: dataDir, Host: "127.0.0.1", Port: 18778}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return New(context.Background(), db, dataDir, app), app, dataDir
}

func TestJobAndTaskDownloads(t *testing.T) {
	h, _, dataDir := exportFixture(t)
	jobURL := "/api/decrypt/jobs/" + exportJob1 + "/download"
	job := mediaRequest(h, "GET", jobURL, "bytes=2-6")
	if job.Code != http.StatusPartialContent || job.Body.String() != "deo-o" || !strings.Contains(job.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("job download = %d %q %v", job.Code, job.Body.String(), job.Header())
	}
	if missing := mediaRequest(h, "GET", "/api/decrypt/jobs/no-such-job/download", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("missing job = %d", missing.Code)
	}
	if incomplete := mediaRequest(h, "GET", "/api/decrypt/jobs/"+exportJob3+"/download", ""); incomplete.Code != http.StatusConflict {
		t.Fatalf("incomplete job = %d", incomplete.Code)
	}
	archive := mediaRequest(h, "GET", "/api/decrypt/tasks/"+exportTaskID+"/download", "")
	if archive.Code != http.StatusOK || archive.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("task ZIP = %d %q", archive.Code, archive.Header().Get("Content-Type"))
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if err != nil || len(reader.File) != 2 {
		t.Fatalf("task ZIP entries = %d, %v", len(reader.File), err)
	}
	want := map[string]string{"same.mp4": "video-one", "same (2).mp4": "video-two"}
	for _, entry := range reader.File {
		in, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(in)
		_ = in.Close()
		if err != nil || string(data) != want[entry.Name] {
			t.Fatalf("ZIP entry %q = %q, %v", entry.Name, data, err)
		}
		delete(want, entry.Name)
	}
	if len(want) != 0 {
		t.Fatal("ZIP lost video files", want)
	}
	if err := os.Remove(filepath.Join(dataDir, "decrypt", exportJob2, "output.mp4")); err != nil {
		t.Fatal(err)
	}
	if broken := mediaRequest(h, "GET", "/api/decrypt/tasks/"+exportTaskID+"/download", ""); broken.Code != http.StatusConflict || broken.Body.Len() == 0 {
		t.Fatalf("missing archive member = %d", broken.Code)
	}
}

func TestLogAndCaptureExports(t *testing.T) {
	h, app, _ := exportFixture(t)
	filtered := mediaRequest(h, "GET", "/api/logs?download=1&source=server&q=issue", "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), "server issue") || strings.Contains(filtered.Body.String(), "capture issue") {
		t.Fatalf("filtered logs = %d %q", filtered.Code, filtered.Body.String())
	}
	if bad := mediaRequest(h, "GET", "/api/logs?download=1&source=invalid", ""); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter = %d", bad.Code)
	}
	jobLog := mediaRequest(h, "GET", "/api/decrypt/jobs/"+exportJob1+"/logs?download=1", "")
	if jobLog.Code != http.StatusOK || jobLog.Body.String() != "first\nsecond\n" {
		t.Fatalf("job logs = %d %q", jobLog.Code, jobLog.Body.String())
	}
	if bad := mediaRequest(h, "GET", "/api/decrypt/jobs/"+exportJob1+"/logs?download=1&offset=100", ""); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid log offset = %d", bad.Code)
	}
	for _, tc := range []struct{ offset, want string }{{"2", "rst\nsecond\n"}, {"6", "second\n"}, {"13", ""}} {
		part := mediaRequest(h, "GET", "/api/decrypt/jobs/"+exportJob1+"/logs?download=1&offset="+tc.offset, "")
		if part.Code != http.StatusOK || part.Body.String() != tc.want {
			t.Errorf("offset %s: status %d, body %q; want %q", tc.offset, part.Code, part.Body.String(), tc.want)
		}
	}
	if absent := mediaRequest(h, "GET", "/api/decrypt/jobs/unknown/logs?download=1", ""); absent.Code != http.StatusNotFound {
		t.Fatalf("deleted job log = %d", absent.Code)
	}
	jsonFile := mediaRequest(h, "GET", "/api/export", "")
	var items []workspace.Capture
	if jsonFile.Code != http.StatusOK || json.Unmarshal(jsonFile.Body.Bytes(), &items) != nil || len(items) != 1 || items[0].ID != "capture-one" {
		t.Fatalf("capture JSON = %d %q", jsonFile.Code, jsonFile.Body.String())
	}
	jsonLines := mediaRequest(h, "GET", "/api/export?format=jsonl", "")
	if jsonLines.Code != http.StatusOK || strings.Count(jsonLines.Body.String(), "\n") != 1 || !strings.Contains(jsonLines.Body.String(), "capture-one") {
		t.Fatalf("capture JSONL = %d %q", jsonLines.Code, jsonLines.Body.String())
	}
	export, err := app.PrepareLogExport(workspace.LogQuery{Source: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Store.InsertLog(store.Log{ID: "later", At: store.Now(), Level: "warn", Source: "server", Message: "late entry"}); err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err := export.Write(context.Background(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.String(), "late entry") || !strings.Contains(snapshot.String(), "server issue") {
		t.Fatal("log export shifted after preparation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := export.Write(ctx, &bytes.Buffer{}); err != context.Canceled {
		t.Fatalf("canceled export = %v", err)
	}
}

func TestCaptureExportPreservesLegacyExtensionFields(t *testing.T) {
	h, app, _ := exportFixture(t)
	const latest = `{"id":"capture-new","phase":"response","method":"GET","url":"https://example.com/x","host":"example.com","headers":{"X-Test":["yes"]},"body":"{\"ok\":true}","futurePayload":{"number":123,"flag":true},"marker":"<legacy>"}`
	if _, err := app.Store.DB.Exec("INSERT INTO captures(payload) VALUES(?)", latest); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"/api/export", "/api/export?format=jsonl"} {
		response := mediaRequest(h, "GET", url, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatalf("%s response = %d, %v", url, response.Code, response.Header())
		}
		var records []map[string]json.RawMessage
		if strings.HasSuffix(url, "jsonl") {
			lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
			if len(lines) != 2 || lines[0] != latest {
				t.Fatalf("%s changed raw JSONL: %q", url, lines)
			}
			for _, line := range lines {
				var record map[string]json.RawMessage
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("%s invalid line: %v", url, err)
				}
				records = append(records, record)
			}
		} else if err := json.Unmarshal(response.Body.Bytes(), &records); err != nil {
			t.Fatalf("%s invalid JSON: %v", url, err)
		}
		if len(records) != 2 || string(records[0]["id"]) != `"capture-new"` || string(records[1]["id"]) != `"capture-one"` ||
			string(records[0]["futurePayload"]) != `{"number":123,"flag":true}` {
			t.Fatalf("%s changed stored records: %+v", url, records)
		}
	}
}
