//go:build darwin

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/gofrs/flock"
)

func verifyMyGoCaptureExport(t *testing.T, w *mygo.Window, manager *downloadManager, done <-chan *mygo.Download) {
	t.Helper()
	type exportResponse struct {
		Status      int    `json:"status"`
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
	}
	read := func(path string) exportResponse {
		t.Helper()
		result, err := mygo.EvalAs[exportResponse](w.Page(), fmt.Sprintf(`(async () => {
			const response=await fetch(%q);
			return {status:response.status, contentType:response.headers.get('content-type') || '', body:await response.text()};
		})()`, "binggan-stream://localhost"+path))
		if err != nil {
			t.Fatalf("capture export %s: %v", path, err)
		}
		return result
	}
	var count int
	if err := mygoE2E.db.DB.QueryRow("SELECT COUNT(*) FROM captures").Scan(&count); err != nil || count != 0 {
		t.Fatalf("isolated capture export fixture is not empty: %d, %v", count, err)
	}
	if empty := read("/api/export"); empty.Status != 200 || strings.TrimSpace(empty.Body) != "[]" {
		t.Fatalf("native empty capture JSON: %+v", empty)
	}
	if empty := read("/api/export?format=jsonl"); empty.Status != 200 || empty.Body != "" {
		t.Fatalf("native empty capture JSONL: %+v", empty)
	}
	const earlier = `{"id":"e2e-capture-earlier","phase":"request","method":"GET","url":"https://example.invalid/one","host":"example.invalid","headers":{},"body":"","legacyField":{"keep":"yes"}}`
	const latest = `{"id":"e2e-capture-latest","phase":"response","method":"POST","url":"https://example.invalid/two","host":"example.invalid","headers":{"x-test":["one"]},"body":"<data>","legacyField":{"keep":"also"}}`
	for _, payload := range []string{earlier, latest} {
		if _, err := mygoE2E.db.DB.Exec("INSERT INTO captures(payload) VALUES(?)", payload); err != nil {
			t.Fatal(err)
		}
	}
	jsonFile := read("/api/export")
	if jsonFile.Status != 200 || !strings.Contains(jsonFile.ContentType, "application/json") {
		t.Fatalf("native capture JSON headers: %+v", jsonFile)
	}
	var records []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonFile.Body), &records); err != nil || len(records) != 2 ||
		string(records[0]["id"]) != `"e2e-capture-latest"` || string(records[1]["id"]) != `"e2e-capture-earlier"` ||
		string(records[0]["legacyField"]) != `{"keep":"also"}` || string(records[1]["legacyField"]) != `{"keep":"yes"}` {
		t.Fatalf("native capture JSON changed stored fields or order: %+v, %v", records, err)
	}
	jsonLines := read("/api/export?format=jsonl")
	if jsonLines.Status != 200 || !strings.Contains(jsonLines.ContentType, "application/x-ndjson") ||
		jsonLines.Body != latest+"\n"+earlier+"\n" {
		t.Fatalf("native capture JSONL changed raw records: %+v", jsonLines)
	}
	destination := filepath.Join(t.TempDir(), "captures.json")
	manager.choose = func(opts mygo.SaveDialogOptions) (string, error) {
		if opts.Title != "保存文件" || filepath.Base(opts.DefaultPath) != "captures.json" {
			return "", fmt.Errorf("unexpected capture export save options: %+v", opts)
		}
		return destination, nil
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const link=document.createElement('a');
		link.href='binggan-stream://localhost/api/export';
		document.body.appendChild(link); link.click(); link.remove(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("capture JSON attachment did not download")
	}
	saved, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(bytes.TrimSpace(saved), bytes.TrimSpace([]byte(jsonFile.Body))) {
		t.Fatalf("saved capture JSON differs from Protocol response: %v", err)
	}
}

func verifyMyGoUntrustedMonitor(t *testing.T, w *mygo.Window) {
	t.Helper()
	// The installed app may hold the shared production TUN lock. This fixture
	// never trusts its CA or starts a TUN, so exercise the lock in its own directory.
	mygoE2E.monitor.op.Lock()
	if mygoE2E.monitor.lock != nil && mygoE2E.monitor.lock.Locked() {
		mygoE2E.monitor.op.Unlock()
		t.Fatal("monitor acquired the production lock before its isolated check")
	}
	mygoE2E.monitor.lock = flock.New(filepath.Join(t.TempDir(), "monitor.lock"))
	mygoE2E.monitor.op.Unlock()
	// Production calls this from Page.OnDOMReady; the opt-in harness creates
	// its own window and starts the monitor only for this isolated check.
	mygoE2E.monitor.domReady(mygoE2E.ctx)
	var state State
	var err error
	readyBy := time.Now().Add(10 * time.Second)
	for time.Now().Before(readyBy) {
		state, err = mygo.EvalAs[State](w.Page(), `window.mygo.call('CaptureService.State')`)
		if err == nil && state.Stage == "certificate" {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if err != nil || state.Stage != "certificate" || !state.TokenConfigured || state.CaptureEnabled {
		t.Fatalf("private untrusted monitor state: stage=%q, message=%q, token=%v, capture=%v, %v", state.Stage, state.Message, state.TokenConfigured, state.CaptureEnabled, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => { await window.mygo.call('CaptureService.Retry'); return true })()`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = mygo.EvalAs[State](w.Page(), `window.mygo.call('CaptureService.State')`)
		if err == nil && state.Stage == "certificate" && !state.CaptureEnabled {
			for _, entry := range state.Logs {
				if entry.Message == "正在重新连接" {
					return
				}
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("untrusted retry did not return to certificate gate: stage=%q, %v", state.Stage, err)
}

func verifyMyGoConnectionDescriptor(t *testing.T, w *mygo.Window) {
	t.Helper()
	valid, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(async () => {
		const status=await window.mygo.call('WorkspaceService.Connection');
		return status.active.provider==='sing-box' && status.active.host==='127.0.0.1' && status.active.port===%d &&
			JSON.stringify(status.active)===JSON.stringify(status.saved) && status.tokenConfigured===true &&
			status.restartRequired===false && status.captureProxyReady===true &&
			status.captureProxyAddress.startsWith('127.0.0.1:') &&
			!Object.keys(status).some(key=>/password|secret|^token$/i.test(key));
	})()`, receiverPort()))
	if err != nil || !valid {
		t.Fatalf("isolated MyGo connection descriptor: %v, %v", valid, err)
	}
}

func verifyMyGoCertificatePage(t *testing.T, w *mygo.Window) {
	t.Helper()
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/certificates"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="证书管理"] dl dd') && !document.querySelector('section[aria-label="证书管理"] [role="status"]')?.innerText.includes('正在检查')`)
	fingerprint, err := mygo.EvalAs[string](w.Page(), `document.querySelectorAll('section[aria-label="证书管理"] dl dd')[1].innerText.trim()`)
	if err != nil || fingerprint == "" {
		t.Fatalf("isolated certificate fingerprint: %q, %v", fingerprint, err)
	}
	if state, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const section=document.querySelector('section[aria-label="证书管理"]');
		return section.innerText.includes('CA 证书尚未安装到系统') &&
			document.querySelector('button[aria-label="将证书安装到系统"]')?.disabled===false &&
			document.querySelector('button[aria-label="信任证书"]')?.disabled===true;
	})()`); err != nil || !state {
		t.Fatalf("new private certificate UI state: %v, %v", state, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="生成新证书"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes('生成新证书？')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes('取消')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]')`)
	unchanged, err := mygo.EvalAs[string](w.Page(), `document.querySelectorAll('section[aria-label="证书管理"] dl dd')[1].innerText.trim()`)
	if err != nil || unchanged != fingerprint {
		t.Fatalf("cancelling private certificate rotation changed fingerprint: %q → %q, %v", fingerprint, unchanged, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll("#workspace-page-actions button")].find(b=>b.textContent.trim()==="刷新状态").click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="证书管理"] dl dd') && !document.querySelector('section[aria-label="证书管理"] [role="status"]')?.innerText.includes('正在检查')`)
	refreshed, err := mygo.EvalAs[string](w.Page(), `document.querySelectorAll('section[aria-label="证书管理"] dl dd')[1].innerText.trim()`)
	if err != nil || refreshed != fingerprint {
		t.Fatalf("refresh changed private certificate fingerprint: %q → %q, %v", fingerprint, refreshed, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="生成新证书"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.innerText.includes('生成新证书？')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent.includes('确认生成')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`!document.querySelector('[role="alertdialog"]') && document.querySelectorAll('section[aria-label="证书管理"] dl dd')[1]?.innerText.trim() !== %q`, fingerprint))
	rotated, err := mygo.EvalAs[string](w.Page(), `document.querySelectorAll('section[aria-label="证书管理"] dl dd')[1].innerText.trim()`)
	if err != nil || rotated == "" || rotated == fingerprint {
		t.Fatalf("private certificate did not rotate: %q → %q, %v", fingerprint, rotated, err)
	}
	if state, err := mygo.EvalAs[bool](w.Page(), `document.querySelector('section[aria-label="证书管理"]')?.innerText.includes('CA 证书尚未安装到系统') && document.querySelector('button[aria-label="将证书安装到系统"]')?.disabled===false`); err != nil || !state {
		t.Fatalf("rotated certificate UI state: %v, %v", state, err)
	}
}
