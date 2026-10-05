package workspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/captureproxy"
	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/store"
)

func testApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	if err := EnsureDirectories(root); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Root: root, DataDir: filepath.Join(root, "data"), Host: "127.0.0.1", Port: 18766, MaxUploadBytes: 1 << 20}
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(cfg, db)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close(); _ = db.Close() })
	return app
}

func TestBindingAuditKeepsFailureDetailsPrivate(t *testing.T) {
	app := testApp(t)
	app.AuditOperation("保存解密配置", 3*time.Millisecond, true, nil)
	app.AuditOperation("选择视频", 7*time.Millisecond, true, errors.New("secret=/Users/person/private.sz"))
	app.AuditOperation("读取密钥历史", time.Millisecond, false, nil)
	page, err := app.Logs(LogQuery{Source: "request", Page: 1, PageSize: 20})
	if err != nil || page.Total != 2 {
		t.Fatalf("audit entries = %+v, %v", page, err)
	}
	for _, entry := range page.Items {
		if entry.RequestID == "" || strings.Contains(entry.Detail, "private.sz") || !strings.Contains(entry.Detail, "MyGo 绑定") {
			t.Fatalf("unsafe audit entry = %+v", entry)
		}
	}
}

func TestCapturePersistenceAndStopBoundary(t *testing.T) {
	app := testApp(t)
	pair := captureproxy.Pair{Request: captureproxy.Event{Phase: "request", URL: "https://learn.shenzaokeji.com/api/test", Host: "learn.shenzaokeji.com", Method: "GET", TS: store.Now()}, Response: captureproxy.Event{Phase: "response", URL: "https://learn.shenzaokeji.com/api/test", Host: "learn.shenzaokeji.com", Method: "GET", TS: store.Now(), Status: 200, Body: "test"}}
	app.collector.Accept(pair)
	if !app.SetCaptureState(true) {
		t.Fatal("capture did not start")
	}
	app.collector.Accept(pair)
	if err := app.ClearTraces("window-one"); !errors.Is(err, errCaptureRunning) {
		t.Fatalf("clear while capturing = %v", err)
	}
	if app.SetCaptureState(false) {
		t.Fatal("capture did not stop")
	}
	app.collector.Accept(pair)
	items, err := app.History(2500)
	if err != nil || len(items) != 2 {
		t.Fatalf("history after stop = %+v, %v", items, err)
	}
	var count int
	if err := app.Store.DB.QueryRow("SELECT COUNT(*) FROM captures").Scan(&count); err != nil || count != 2 {
		t.Fatalf("saved count = %d, %v", count, err)
	}
	health, err := app.Health()
	if err != nil || !health.OK || health.Count != 2 || health.CaptureEnabled {
		t.Fatalf("health = %+v, %v", health, err)
	}
	if err := app.ClearTraces("window-one"); err != nil {
		t.Fatal(err)
	}
	items, err = app.History(2500)
	if err != nil || len(items) != 0 {
		t.Fatalf("clear left captures: %+v, %v", items, err)
	}
	if _, err := app.History(2501); err == nil {
		t.Fatal("history limit accepted")
	}
}

func TestSettingsAndLogsThroughServices(t *testing.T) {
	app := testApp(t)
	if _, err := app.SaveSettings(PlayerSettings{Mode: "manual", SoftwareName: "test", AppMD5: "bad"}); err == nil {
		t.Fatal("invalid checksum accepted")
	}
	saved, err := app.SaveSettings(PlayerSettings{Mode: "manual", SoftwareName: " test ", AppMD5: strings.Repeat("A", 32)})
	if err != nil || saved.SoftwareName != "test" || saved.AppMD5 != strings.Repeat("a", 32) {
		t.Fatalf("saved settings = %+v, %v", saved, err)
	}
	connection, err := app.Connection()
	if err != nil || connection.RestartRequired || connection.Active.Port != 18766 {
		t.Fatalf("connection = %+v, %v", connection, err)
	}
	entry := ClientLog{ID: "stable-entry", At: store.Now(), Level: "error", Source: "connection", Message: "connection broke", Detail: "full details"}
	for range 2 {
		if err := app.AppendLogs([]ClientLog{entry}); err != nil {
			t.Fatal(err)
		}
	}
	page := logPage(t, app, LogQuery{Source: "connection", Search: "broke"})
	if page.Total != 1 || page.Items[0].Detail != "full details" {
		t.Fatalf("deduplicated logs = %+v", page)
	}
	entry.ID, entry.Source = "legacy-connection-entry", ""
	if err := app.AppendLogs([]ClientLog{entry}); err != nil {
		t.Fatal(err)
	}
	export, err := app.PrepareLogExport(LogQuery{Source: "connection"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := export.Write(context.Background(), &output); err != nil || !strings.Contains(output.String(), "full details") {
		t.Fatalf("log export = %q, %v", output.String(), err)
	}
	if _, err := app.Logs(LogQuery{Page: -1, PageSize: 20}); err == nil {
		t.Fatal("invalid page accepted")
	}
	if _, err := app.PrepareLogExport(LogQuery{From: "broken"}); err == nil {
		t.Fatal("invalid date accepted")
	}
	diagnosis, err := app.Diagnose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]DiagnosticCheck{}
	seen := map[string]bool{}
	previous := ""
	for _, check := range diagnosis.Checks {
		if check.Group == "" {
			t.Fatalf("check %q has no group", check.Name)
		}
		// A group is contiguous: once it has ended it must not reappear.
		if check.Group != previous && seen[check.Group] {
			t.Fatalf("group %q is split: %+v", check.Group, diagnosis.Checks)
		}
		seen[check.Group], previous = true, check.Group
		names[check.Name] = check
	}
	for _, name := range []string{"Go 运行环境", "处理器架构", "sing-box", "SQLite 数据库", "Go 解密引擎"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("diagnosis is missing %q: %+v", name, diagnosis.Checks)
		}
	}
}

func TestTaskEndToEndLogOffsetsAndIndependentResource(t *testing.T) {
	app := testApp(t)
	source, err := os.ReadFile("../engine/testdata/encrypted.sz")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.Config.Root, "input", "fixture.sz"), source, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("../engine/testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.CreateTask(CreateTaskInput{Name: "测试任务", Videos: []TaskVideoInput{{SourceID: "fixture.sz", KeyJSON: string(key)}}})
	if err != nil || len(created.Children) != 1 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	jobID := created.Children[0].ID
	task := waitTask(t, app, created.ID)
	if task.Completed != 1 || task.Children[0].Status != "completed" {
		t.Fatalf("task did not complete: %+v", task)
	}
	if _, err := os.Stat(app.Tasks.Path(jobID, "output.mp4")); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("完整日志，", 60000)
	if err := app.Store.AppendJobLog(jobID, "INFO", long); err != nil {
		t.Fatal(err)
	}
	var received []byte
	for offset := int64(0); ; {
		chunk, err := app.JobLogs(jobID, offset)
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil || chunk.Offset != offset+int64(len(data)) {
			t.Fatalf("log offset = %+v, %v", chunk, err)
		}
		received = append(received, data...)
		offset = chunk.Offset
		if !chunk.HasMore {
			break
		}
	}
	export, err := app.PrepareJobLogExport(jobID)
	if err != nil {
		t.Fatal(err)
	}
	var all bytes.Buffer
	if err := export.Write(context.Background(), &all); err != nil || !bytes.Equal(received, all.Bytes()) || !strings.Contains(all.String(), long) {
		t.Fatalf("full job log differs from chunks: %v", err)
	}
	deleted, err := app.DeleteTasks([]string{created.ID, created.ID})
	if err != nil || len(deleted.Deleted) != 1 || deleted.Deleted[0] != created.ID || deleted.CleanupPending {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	if _, err := app.JobLogs(jobID, 0); err == nil {
		t.Fatal("deleted job log remained")
	}
	page, err := app.Resources(ResourceQuery{Page: 1, PageSize: 20, Search: "fixture"})
	if err != nil || page.Total != 1 || page.Items[0].ID != jobID {
		t.Fatalf("deleting task removed published resource: %+v, %v", page, err)
	}
	if _, err := os.Stat(app.Tasks.Path(jobID, "")); !os.IsNotExist(err) {
		t.Fatalf("task files remained: %v", err)
	}
}

func TestSafeFileRejectsTraversalAndSymlink(t *testing.T) {
	app := testApp(t)
	if _, err := safeFile(app.Config.Root, "../secret.sz", ".sz"); err == nil {
		t.Fatal("path traversal accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.sz")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(app.Config.Root, "input", "link.sz")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeFile(filepath.Join(app.Config.Root, "input"), "link.sz", ".sz"); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestRetiredConnectionMigratesWithoutDeletingRecords(t *testing.T) {
	app := testApp(t)
	if err := app.Store.SetSetting("connection", Connection{Provider: "legacy-external", Host: "192.0.2.1", Port: 9999}); err != nil {
		t.Fatal(err)
	}
	original := `{"id":"saved-record","source":"legacy-external","body":"retained"}`
	if _, err := app.Store.DB.Exec("INSERT INTO captures(payload) VALUES(?)", original); err != nil {
		t.Fatal(err)
	}
	status, err := app.Connection()
	if err != nil || status.Active.Provider != "sing-box" || status.Saved != status.Active {
		t.Fatalf("connection did not migrate: %+v, %v", status, err)
	}
	var saved Connection
	if err := app.Store.GetSetting("connection", &saved); err != nil || saved != status.Active {
		t.Fatalf("saved connection = %+v, %v", saved, err)
	}
	var payload string
	if err := app.Store.DB.QueryRow("SELECT payload FROM captures").Scan(&payload); err != nil || payload != original {
		t.Fatalf("historical capture changed: %s, %v", payload, err)
	}
}

func TestConnectionReportsOfflineAndTokenState(t *testing.T) {
	app := testApp(t)
	status, err := app.Connection()
	if err != nil || status.TokenConfigured || status.CaptureProxyReady || status.CaptureProxyAddress != "" || status.RestartRequired {
		t.Fatalf("offline connection = %+v, %v", status, err)
	}
	app.Config.Token = "private-test-token"
	status, err = app.Connection()
	if err != nil || !status.TokenConfigured || status.CaptureProxyReady || status.CaptureProxyAddress != "" {
		t.Fatalf("token connection = %+v, %v", status, err)
	}
	encoded, err := json.Marshal(status)
	if err != nil || bytes.Contains(encoded, []byte("private-test-token")) {
		t.Fatalf("connection descriptor exposed a token: %v", err)
	}
}

func TestServiceDTOsContainNoPrivateRetryKey(t *testing.T) {
	app := testApp(t)
	result, err := app.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "getPwdData") || strings.Contains(string(encoded), "passwords") {
		t.Fatalf("task DTO leaks private keys: %s, %v", encoded, err)
	}
}
