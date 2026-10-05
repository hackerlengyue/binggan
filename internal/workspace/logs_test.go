package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/store"
)

func logPage(t *testing.T, app *App, query LogQuery) LogPage {
	t.Helper()
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	page, err := app.Logs(query)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func clientEntry(entry store.Log) ClientLog {
	return ClientLog{entry.ID, entry.At, entry.Level, entry.Source, entry.Message, entry.Detail}
}

func TestUnifiedLogSourcesSortSearchAndExport(t *testing.T) {
	app := testApp(t)
	for i, source := range []string{"server", "capture", "certificate"} {
		at := "2026-09-30T01:00:00Z"
		if i > 0 {
			at = fmt.Sprintf("2026-09-30T01:00:00.%dZ", i)
		}
		if err := app.Store.InsertLog(store.Log{ID: source, At: at, Level: "info", Source: source, Message: "unified " + source, Detail: "searchable detail"}); err != nil {
			t.Fatal(err)
		}
	}
	page := logPage(t, app, LogQuery{})
	if page.Total != 3 || page.Items[0].ID != "certificate" || page.Items[2].ID != "server" {
		t.Fatalf("log order = %+v", page)
	}
	for _, source := range []string{"capture", "certificate"} {
		page = logPage(t, app, LogQuery{Source: source, Search: "searchable"})
		if page.Total != 1 || page.Items[0].Message != "unified "+source {
			t.Fatalf("filter %s = %+v", source, page)
		}
	}
	export, err := app.PrepareLogExport(LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := export.Write(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"server", "capture", "certificate"} {
		if !strings.Contains(output.String(), "unified "+source) {
			t.Fatalf("export omitted %s", source)
		}
	}
	app.collector.Log("info", "collector info is persistent", "capture detail")
	if page := logPage(t, app, LogQuery{Search: "collector"}); page.Total != 1 {
		t.Fatalf("collector log = %+v", page)
	}
}

func TestUnifiedLogClearFailurePreservesPersistentEntries(t *testing.T) {
	app := testApp(t)
	if err := app.Store.Log("info", "capture", "preserved record", "", ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	app.SetLogClearer(func() error { calls++; return errors.New("native clear failed") })
	if _, err := app.ClearLogs(""); err == nil || calls != 0 {
		t.Fatalf("invalid scope touched native logs: %v, calls %d", err, calls)
	}
	if _, err := app.ClearLogs("all"); err == nil || calls != 1 {
		t.Fatalf("native clear failure = %v, calls %d", err, calls)
	}
	if page := logPage(t, app, LogQuery{}); page.Total != 1 || page.Items[0].Message != "preserved record" {
		t.Fatalf("clear failure removed records: %+v", page)
	}
}

func TestSystemLogPagination(t *testing.T) {
	app := testApp(t)
	for i := range 121 {
		if err := app.Store.InsertLog(store.Log{ID: fmt.Sprint(i), At: store.Now(), Level: "info", Source: "server", Message: "pagination fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, size := range []int{20, 50, 100} {
		seen := map[string]bool{}
		for number := 1; number <= (121+size-1)/size; number++ {
			page := logPage(t, app, LogQuery{Page: number, PageSize: size})
			if page.Page != number || page.PageSize != size || page.Total != 121 || len(page.Items) != min(size, 121-(number-1)*size) {
				t.Fatalf("incorrect page: %+v", page)
			}
			for _, item := range page.Items {
				if seen[item.ID] {
					t.Fatalf("duplicate across pages: %s", item.ID)
				}
				seen[item.ID] = true
			}
		}
		if len(seen) != 121 {
			t.Fatal("records lost across pages")
		}
	}
	if page := logPage(t, app, LogQuery{Page: 999, PageSize: 100}); page.Page != 2 {
		t.Fatalf("page not clamped: %+v", page)
	}
	for _, size := range []int{0, -1, 1000} {
		if _, err := app.Logs(LogQuery{Page: 1, PageSize: size}); err == nil {
			t.Fatalf("invalid size %d accepted", size)
		}
	}
	if page := logPage(t, app, LogQuery{Search: "no-such-entry", Page: 2}); page.Page != 1 || page.Total != 0 {
		t.Fatalf("empty page = %+v", page)
	}
}

func TestClearSystemLogsPreservesJobsAndRejectsDelayedReplay(t *testing.T) {
	app := testApp(t)
	old := store.Log{ID: store.ID(), At: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), Level: "info", Source: "connection", Message: "old connection entry"}
	if err := app.Store.InsertLog(old); err != nil {
		t.Fatal(err)
	}
	if err := app.Store.SetSetting("review-setting", "preserved"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.DB.Exec("INSERT INTO tasks VALUES('test-task','{}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.DB.Exec("INSERT INTO jobs VALUES('test-job','test-task','{}')"); err != nil {
		t.Fatal(err)
	}
	if err := app.Store.AppendJobLog("test-job", "INFO", "preserved child log"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ClearLogs(""); err == nil {
		t.Fatal("empty clear scope accepted")
	}
	if count, err := app.ClearLogs("all"); err != nil || count < 1 {
		t.Fatalf("clear result = %d, %v", count, err)
	}
	if page := logPage(t, app, LogQuery{}); page.Total != 0 {
		t.Fatalf("deleted logs remained: %+v", page)
	}
	export, err := app.PrepareLogExport(LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := export.Write(context.Background(), &output); err != nil || output.Len() != 0 {
		t.Fatalf("deleted logs exported: %q, %v", output.String(), err)
	}
	var n int
	if err := app.Store.DB.QueryRow("SELECT count(*) FROM job_logs WHERE job_id='test-job'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("child logs deleted: %d, %v", n, err)
	}
	var setting string
	if err := app.Store.GetSetting("review-setting", &setting); err != nil || setting != "preserved" {
		t.Fatalf("settings changed: %q, %v", setting, err)
	}
	if err := app.AppendLogs([]ClientLog{clientEntry(old)}); err != nil {
		t.Fatal(err)
	}
	if page := logPage(t, app, LogQuery{}); page.Total != 0 {
		t.Fatalf("stale entry returned: %+v", page)
	}
	fresh := old
	fresh.ID, fresh.At, fresh.Message = store.ID(), store.Now(), "new connection entry"
	if err := app.AppendLogs([]ClientLog{clientEntry(fresh)}); err != nil {
		t.Fatal(err)
	}
	if page := logPage(t, app, LogQuery{}); page.Total != 1 || page.Items[0].Message != fresh.Message {
		t.Fatalf("new logs stopped after clear: %+v", page)
	}
}

func TestClearConnectionLogsLeavesOtherSources(t *testing.T) {
	app := testApp(t)
	old := store.Log{ID: store.ID(), At: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), Level: "info", Source: "connection", Message: "old connection entry"}
	if err := app.Store.InsertLog(old); err != nil {
		t.Fatal(err)
	}
	if err := app.Store.Log("info", "server", "keep server log", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ClearLogs("connection"); err != nil {
		t.Fatal(err)
	}
	if page := logPage(t, app, LogQuery{Source: "connection"}); page.Total != 0 {
		t.Fatalf("connection logs remained: %+v", page)
	}
	if page := logPage(t, app, LogQuery{Source: "server"}); page.Total != 1 || page.Items[0].Message != "keep server log" {
		t.Fatalf("server logs removed: %+v", page)
	}
	if err := app.AppendLogs([]ClientLog{clientEntry(old)}); err != nil {
		t.Fatal(err)
	}
	if page := logPage(t, app, LogQuery{Source: "connection"}); page.Total != 0 {
		t.Fatalf("old connection log returned: %+v", page)
	}
	fresh := old
	fresh.ID, fresh.At, fresh.Message = store.ID(), store.Now(), "new connection entry"
	if err := app.AppendLogs([]ClientLog{clientEntry(fresh)}); err != nil {
		t.Fatal(err)
	}
	if page := logPage(t, app, LogQuery{Source: "connection"}); page.Total != 1 || page.Items[0].Message != fresh.Message {
		t.Fatalf("fresh connection log missing: %+v", page)
	}
}

func TestClearSystemLogsRollsBackOnDatabaseFailure(t *testing.T) {
	app := testApp(t)
	if err := app.Store.Log("info", "server", "keep on failure", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.DB.Exec("CREATE TRIGGER fail_clear BEFORE INSERT ON settings WHEN NEW.key='logs_cleared_at' BEGIN SELECT RAISE(FAIL,'injected clear failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ClearLogs("all"); err == nil {
		t.Fatal("clear failure not reported")
	}
	var n int
	if err := app.Store.DB.QueryRow("SELECT count(*) FROM system_logs WHERE message='keep on failure'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("clear was not atomic: %d, %v", n, err)
	}
}
