//go:build darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/store"
)

func verifyMyGoHistory(t *testing.T, w *mygo.Window, manager *downloadManager, done <-chan *mygo.Download) {
	t.Helper()
	previous, err := mygo.EvalAs[string](w.Page(), `localStorage.getItem("shz_key_history_v1") || ""`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
			const previous=%q;
			if (previous) localStorage.setItem("shz_key_history_v1", previous);
			else localStorage.removeItem("shz_key_history_v1");
			return true;
		})()`, previous))
	}()
	validPayload := map[string]any{
		"passwords":  map[string]string{"1": "1234567890abcdef1234567890abcdef"},
		"getPwdData": map[string]any{"密码": "cipher", "uit": "test-user", "den": 1200, "pmn": 20, "pattern": "3", "whenLong": 0},
	}
	invalidPayload := map[string]any{
		"passwords":  map[string]string{},
		"getPwdData": map[string]any{"密码": "", "uit": "", "den": 0, "pmn": 0, "pattern": "", "whenLong": 0},
	}
	entries := []map[string]any{
		{"id": "e2e-history-alpha", "name": "E2E history alpha", "createdAt": "2026-10-02T04:00:00Z", "updatedAt": "2026-10-02T04:00:00Z", "keyJson": validPayload, "traces": []any{}},
		{"id": "e2e-history-beta", "name": "E2E history beta", "createdAt": "2026-10-02T03:59:00Z", "updatedAt": "2026-10-02T03:59:00Z", "keyJson": validPayload, "traces": []any{}},
		{"id": "e2e-history-incomplete", "name": "E2E history incomplete", "createdAt": "2026-10-02T03:58:00Z", "updatedAt": "2026-10-02T03:58:00Z", "keyJson": invalidPayload, "traces": []any{}},
	}
	if err := mygoE2E.app.Store.SetSetting("key_history_v1", map[string]any{"version": 1, "items": entries, "deletedCaptureIds": []string{}}); err != nil {
		t.Fatal(err)
	}
	before, err := mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		localStorage.setItem("shz_key_history_v1", JSON.stringify({version:1,items:[],deletedCaptureIds:[]}));
		location.hash="#/history";
		location.reload();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin > %f && location.hash === "#/history" && document.querySelector('section[aria-label="秘钥管理"]')?.innerText.includes("E2E history incomplete")`, before))
	if rows, err := mygo.EvalAs[int](w.Page(), `document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length`); err != nil || rows != 3 {
		t.Fatalf("isolated key history list: rows=%d, %v", rows, err)
	}
	if valid, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const rows=[...document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr')];
		const bad=rows.find(r=>r.innerText.includes('E2E history incomplete'));
		return !!bad && bad.innerText.includes('无密码') && bad.querySelector('button[aria-label^="下载"]')?.disabled &&
			rows.filter(r=>r.querySelector('button[aria-label^="下载"]:not([disabled])')).length===2;
	})()`); err != nil || !valid {
		t.Fatalf("isolated history validity indicators: %v, %v", valid, err)
	}
	captureMyGoThemes(t, w, "history-status")
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索记录名称"]');
		input.value='alpha'; input.dispatchEvent(new Event('input',{bubbles:true})); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length===1`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索记录名称"]');
		input.value='no such record'; input.dispatchEvent(new Event('input',{bubbles:true})); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="秘钥管理"]')?.innerText.includes('没有匹配的记录')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索记录名称"]');
		input.value=''; input.dispatchEvent(new Event('input',{bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('button[aria-label="改名 E2E history alpha"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="改名 E2E history alpha"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes('修改记录名称')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('#capture-file-name');
		input.value='Canceled rename'; input.dispatchEvent(new Event('input',{bubbles:true}));
		[...document.querySelectorAll('[role="dialog"] button')].find(b=>b.textContent.includes('取消')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="dialog"]') && document.querySelector('button[aria-label="改名 E2E history alpha"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="改名 E2E history alpha"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes('修改记录名称')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('#capture-file-name');
		input.value='E2E renamed alpha'; input.dispatchEvent(new Event('input',{bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => {
		await new Promise(resolve => setTimeout(resolve, 50));
		[...document.querySelectorAll('[role="dialog"] button')].find(b=>b.textContent.trim()==='保存').click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="dialog"]') && document.querySelector('button[aria-label="改名 E2E renamed alpha"]')`)
	var saved struct {
		Items []struct{ ID, Name string } `json:"items"`
	}
	if err := mygoE2E.app.Store.GetSetting("key_history_v1", &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Items) != 3 || saved.Items[0].ID != "e2e-history-alpha" || saved.Items[0].Name != "E2E renamed alpha" {
		t.Fatalf("renamed history did not persist: %+v", saved.Items)
	}
	destination := filepath.Join(t.TempDir(), "renamed.json")
	manager.choose = func(opts mygo.SaveDialogOptions) (string, error) {
		if opts.Title != "保存文件" || filepath.Base(opts.DefaultPath) != "E2E renamed alpha.json" {
			return "", fmt.Errorf("unexpected history download options: %+v", opts)
		}
		return destination, nil
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="下载 E2E renamed alpha"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("history JSON did not download")
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Passwords map[string]string `json:"passwords"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Passwords["1"] != "1234567890abcdef1234567890abcdef" {
		t.Fatalf("history JSON content: %+v, %v", payload, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('a[href="#/history/e2e-history-incomplete"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash==="#/history/e2e-history-incomplete" && document.querySelector('section[aria-label="秘钥管理"]')?.innerText.includes('找不到这条记录')===false`)
	if disabled, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const section=document.querySelector('section[aria-label="秘钥管理"]');
		const actions=[...section.querySelectorAll('button')].filter(b=>['解密视频','下载 JSON'].includes(b.textContent.trim()));
		return actions.length===2 && actions.every(b=>b.disabled);
	})()`); err != nil || !disabled {
		t.Fatalf("incomplete history action state: %v, %v", disabled, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('a[aria-label="返回秘钥管理"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash==="#/history" && document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length===3`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="删除 E2E history beta"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes('删除这条记录？')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes('取消')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]') && document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length===3`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		document.querySelector('[role="checkbox"][aria-label="选择 E2E renamed alpha"]').click();
		document.querySelector('[role="checkbox"][aria-label="选择 E2E history beta"]').click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="秘钥管理"] [role="status"]')?.innerText.includes('已选 2 条')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="秘钥管理"] button')].find(b=>b.textContent.includes('删除所选')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes('删除所选的 2 条记录？')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes('确认删除')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]') && document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length===1`)
	var afterBatch struct {
		Items             []struct{ ID string } `json:"items"`
		DeletedCaptureIDs []string              `json:"deletedCaptureIds"`
	}
	if err := mygoE2E.app.Store.GetSetting("key_history_v1", &afterBatch); err != nil {
		t.Fatal(err)
	}
	if len(afterBatch.Items) != 1 || afterBatch.Items[0].ID != "e2e-history-incomplete" ||
		!slices.Contains(afterBatch.DeletedCaptureIDs, "e2e-history-alpha") || !slices.Contains(afterBatch.DeletedCaptureIDs, "e2e-history-beta") {
		t.Fatalf("batch deletion not durable: %+v", afterBatch)
	}
	beforeReload, err := mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.reload(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin > %f && location.hash==="#/history" && document.querySelectorAll('section[aria-label="秘钥管理"] table tbody tr').length===1`, beforeReload))
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="删除 E2E history incomplete"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes('删除这条记录？')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes('确认删除')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]') && document.querySelector('section[aria-label="秘钥管理"]')?.innerText.includes('还没有提取记录')`)
	var empty struct {
		Items             []json.RawMessage `json:"items"`
		DeletedCaptureIDs []string          `json:"deletedCaptureIds"`
	}
	if err := mygoE2E.app.Store.GetSetting("key_history_v1", &empty); err != nil || len(empty.Items) != 0 || len(empty.DeletedCaptureIDs) != 3 {
		t.Fatalf("single deletion not durable: %+v, %v", empty, err)
	}
}

func verifyMyGoLogClear(t *testing.T, w *mygo.Window) {
	t.Helper()
	if err := mygoE2E.app.Store.InsertLog(store.Log{ID: store.ID(), At: store.Now(), Level: "info", Source: "server", Message: "isolated log clear fixture"}); err != nil {
		t.Fatal(err)
	}
	var before, jobLogs int
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM system_logs").Scan(&before); err != nil || before < 1 {
		t.Fatalf("log fixture missing: count=%d, %v", before, err)
	}
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM job_logs").Scan(&jobLogs); err != nil || jobLogs < 1 {
		t.Fatalf("child log fixture missing: count=%d, %v", jobLogs, err)
	}
	w.SetSize(1000, 680)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/logs"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="日志查询"]')?.innerText.includes("isolated log clear fixture")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="清空日志"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes("清空全部日志？")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes("取消")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]')`)
	var retainedFixture int
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM system_logs WHERE message='isolated log clear fixture'").Scan(&retainedFixture); err != nil || retainedFixture != 1 {
		t.Fatalf("cancelling clear removed its fixture: count=%d, %v", retainedFixture, err)
	}
	mygoE2E.app.SetLogClearer(func() error { return errors.New("injected native log clear failure") })
	defer mygoE2E.app.SetLogClearer(mygoE2E.monitor.clearLogbook)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="清空日志"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes("清空全部日志？")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes("确认清空")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"] [role="alert"]')?.innerText.includes("injected native log clear failure")`)
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM system_logs WHERE message='isolated log clear fixture'").Scan(&retainedFixture); err != nil || retainedFixture != 1 {
		t.Fatalf("failed native clear removed its fixture: count=%d, %v", retainedFixture, err)
	}
	mygoE2E.app.SetLogClearer(mygoE2E.monitor.clearLogbook)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes("确认清空")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]')`)
	var after, clearAudit, keptChildLogs int
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM system_logs WHERE message='isolated log clear fixture'").Scan(&after); err != nil || after != 0 {
		t.Fatalf("clear retained an old system log: count=%d, %v", after, err)
	}
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM system_logs WHERE message='清理日志成功'").Scan(&clearAudit); err != nil || clearAudit != 0 {
		t.Fatalf("clear operation wrote itself back into system logs: count=%d, %v", clearAudit, err)
	}
	if err := mygoE2E.app.Store.DB.QueryRow("SELECT count(*) FROM job_logs").Scan(&keptChildLogs); err != nil || keptChildLogs != jobLogs {
		t.Fatalf("clear touched child logs: before=%d, after=%d, %v", jobLogs, keptChildLogs, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索日志"]');
		input.value="isolated log clear fixture";
		input.dispatchEvent(new Event('input',{bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="日志查询"]')?.innerText.includes("暂无匹配日志")`)
	captureMyGoThemes(t, w, "logs-empty")
}

func captureMyGoThemes(t *testing.T, w *mygo.Window, name string) {
	t.Helper()
	out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT")
	if out == "" {
		return
	}
	dark, err := mygo.EvalAs[bool](w.Page(), `document.documentElement.classList.contains('dark')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`document.documentElement.classList.toggle('dark', %t)`, dark))
	}()
	for _, theme := range []string{"light", "dark"} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`document.documentElement.classList.toggle('dark', %t)`, theme == "dark")); err != nil {
			t.Fatal(err)
		}
		// Allow inherited color transitions to finish before capturing WKWebView.
		time.Sleep(400 * time.Millisecond)
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-"+name+"-"+theme+".png", png, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
