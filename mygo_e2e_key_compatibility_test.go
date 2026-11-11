//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestMyGoLegacyKeyImport(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("set BINGGAN_MYGO_E2E=1 to run the native WebView test")
	}
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("workspace startup timed out")
	}
	previous, err := mygoE2E.app.KeyHistory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mygoE2E.app.Store.SetSetting("key_history_v1", json.RawMessage(previous)) }()
	if err := mygoE2E.app.Store.SetSetting("key_history_v1", map[string]any{"version": 1, "items": []any{}}); err != nil {
		t.Fatal(err)
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Legacy key import E2E", URL: "/#/history", Width: 1320, Height: 880})
	defer w.Destroy()
	waitMyGoPage(t, w, `document.querySelector('input[aria-label="上传密钥"]:not([disabled])')`)
	raw, err := os.ReadFile("internal/engine/testdata/key-legacy-windows.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
		const input=document.querySelector('input[aria-label="上传密钥"]');
		Object.defineProperty(input,'files',{configurable:true,value:[new File([%q],'Legacy E2E.json',{type:'application/json'})]});
		input.dispatchEvent(new Event('change',{bubbles:true}));return true;
	})()`, string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('button[aria-label="下载 Legacy E2E"]') && !document.querySelector('input[aria-label="上传密钥"]').disabled`)
	stored, err := mygoE2E.app.KeyHistory()
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Items []struct {
			KeyJSON struct {
				Data map[string]any `json:"getPwdData"`
			} `json:"keyJson"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stored), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 || history.Items[0].KeyJSON.Data["appMd5"] != "a93b82a7f6277f8a30a48f460dcb87c2" {
		t.Fatal("uploaded key lost the matched historical hash")
	}
	before, err := mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mygo.EvalAs[bool](w.Page(), `(() => {location.reload();return true})()`)
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin>%f && document.querySelector('button[aria-label="下载 Legacy E2E"]')`, before))
	manager := installDownloads(w)
	defer manager.close()
	destination := filepath.Join(t.TempDir(), "legacy-export.json")
	manager.choose = func(mygo.SaveDialogOptions) (string, error) { return destination, nil }
	done := make(chan *mygo.Download, 1)
	w.Page().OnDownloadDone(func(d *mygo.Download) { done <- d })
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {document.querySelector('button[aria-label="下载 Legacy E2E"]').click();return true})()`); err != nil {
		t.Fatal(err)
	}
	select {
	case download := <-done:
		if download.Err != nil {
			t.Fatal(download.Err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("prepared key export did not finish")
	}
	exported, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Data map[string]any `json:"getPwdData"`
	}
	if err := json.Unmarshal(exported, &document); err != nil {
		t.Fatal(err)
	}
	if document.Data["appMd5"] != "a93b82a7f6277f8a30a48f460dcb87c2" || document.Data["uit"] != "synthetic-legacy-capture" {
		t.Fatal("reload/export lost matched parameters or replaced original capture")
	}
	t.Log("native upload, compatibility matching, durable reload and JSON export passed")
}
