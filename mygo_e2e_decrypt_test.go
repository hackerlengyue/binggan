//go:build darwin

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/workspace"
)

func verifyMyGoVideoImportChannel(t *testing.T, w *mygo.Window) {
	t.Helper()
	dir := t.TempDir()
	validPath := filepath.Join(dir, "native-import.sz")
	content := bytes.Repeat([]byte("isolated MyGo import fixture\n"), 8192)
	if err := os.WriteFile(validPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(dir, "empty.sz")
	wrongPath := filepath.Join(dir, "wrong.txt")
	linkPath := filepath.Join(dir, "linked.sz")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrongPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(validPath, linkPath); err != nil {
		t.Fatal(err)
	}
	paths, err := json.Marshal([]string{validPath, emptyPath, wrongPath, linkPath})
	if err != nil {
		t.Fatal(err)
	}
	choices, err := mygo.EvalAs[[]workspace.LocalVideo](w.Page(), fmt.Sprintf(`window.mygo.call('WorkspaceService.InspectVideoFiles',%s)`, paths))
	if err != nil || len(choices) != 4 || choices[0].Error != "" || choices[0].Size != int64(len(content)) ||
		choices[1].Error == "" || choices[2].Error == "" || choices[3].Error == "" {
		t.Fatalf("native WebView file inspection: %+v, %v", choices, err)
	}
	type importResult struct {
		Result   workspace.ImportedVideo         `json:"result"`
		Progress []workspace.VideoImportProgress `json:"progress"`
	}
	imported, err := mygo.EvalAs[importResult](w.Page(), fmt.Sprintf(`(async () => {
		const choice=(await window.mygo.call('WorkspaceService.InspectVideoFiles',[%q]))[0];
		const progress=[];
		const updates=window.mygo.channel(value=>progress.push(value));
		const result=await window.mygo.call('WorkspaceService.ImportVideo',choice,updates);
		return {result,progress};
	})()`, validPath))
	if err != nil || imported.Result.ID == "" || imported.Result.Name != "native-import.sz" ||
		len(imported.Progress) < 2 || imported.Progress[0].Copied != 0 ||
		imported.Progress[len(imported.Progress)-1].Copied != int64(len(content)) {
		t.Fatalf("native WebView import progress: %+v, %v", imported, err)
	}
	upload, ok := mygoE2E.app.Tasks.Upload(imported.Result.ID)
	if !ok {
		t.Fatal("native WebView import did not register its upload")
	}
	copied, err := os.ReadFile(upload.Path)
	if err != nil || !bytes.Equal(copied, content) {
		t.Fatalf("native WebView imported bytes differ: %d, %v", len(copied), err)
	}
}

func verifyMyGoDecryptFlow(t *testing.T, w *mygo.Window) {
	t.Helper()
	for _, fixture := range []struct{ source, target string }{
		{"internal/engine/testdata/encrypted.sz", filepath.Join(mygoE2E.app.Config.Root, "input", "fixture.sz")},
		{"internal/engine/testdata/key.json", filepath.Join(mygoE2E.app.Config.Root, "keys", "fixture.json")},
	} {
		body, err := os.ReadFile(fixture.source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture.target, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w.SetSize(1000, 680)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/decrypt"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/decrypt" && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部0")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('#workspace-page-actions button')].find(b => b.textContent.includes("新增解密")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes("fixture.sz")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const dialog=document.querySelector('[role="dialog"]');
		[...dialog.querySelectorAll('label')].find(l => l.textContent.includes("fixture.sz")).querySelector('[role="checkbox"]').click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.textContent.replace(/\s+/g,"").includes("已选1个视频")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('[role="dialog"] button')].find(b => b.textContent.includes("下一步")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes("密钥已匹配") && document.querySelector('[role="dialog"]')?.innerText.includes("自动匹配")`)
	ready, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const button=[...document.querySelectorAll('[role="dialog"] button')].find(b => b.textContent.includes("创建任务（1 个视频）"));
		return !!button && !button.disabled;
	})()`)
	if err != nil || !ready {
		t.Fatalf("matched video cannot be submitted: ready=%v, %v", ready, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('[role="dialog"] button')].find(b => b.textContent.includes("创建任务（1 个视频）")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash.startsWith("#/decrypt?task=") && !document.querySelector('[role="dialog"]')`)
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("成功1·失败0")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('section[aria-label="视频解密"] button')].find(b => b.textContent.includes("返回任务列表")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/decrypt" && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部1") && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("已完成1")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/decrypt?task=missing-e2e"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="视频解密"]')?.innerText.includes("未找到该任务")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('section[aria-label="视频解密"] button')].find(b => b.textContent.includes("返回任务列表")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/decrypt" && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部1")`)
}

func verifyMyGoRetryAndDelete(t *testing.T, w *mygo.Window) {
	t.Helper()
	brokenPath := filepath.Join(mygoE2E.app.Config.Root, "input", "broken.sz")
	if err := os.WriteFile(brokenPath, []byte("broken video"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("internal/engine/testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	task, err := mygoE2E.app.CreateTask(workspace.CreateTaskInput{
		Name:   "E2E retry task",
		Videos: []workspace.TaskVideoInput{{SourceID: "broken.sz", KeyJSON: string(key)}},
	})
	if err != nil || len(task.Children) != 1 {
		t.Fatalf("create broken task: %+v, %v", task, err)
	}
	jobID := task.Children[0].ID
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := mygoE2E.app.Tasks.Task(task.ID)
		if ok && view.Status == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if view, ok := mygoE2E.app.Tasks.Task(task.ID); !ok || view.Status != "failed" || !view.Children[0].Retryable {
		t.Fatalf("broken task did not fail with retry input: %+v, %v", view, ok)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/overview"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/overview"`)
	if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => { location.hash=%q; return true })()`, "#/decrypt?task="+task.ID)); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="视频解密"]')?.innerText.includes("broken.sz") && document.querySelector('section[aria-label="视频解密"]')?.innerText.includes("失败")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('section[aria-label="视频解密"] button[aria-label^="查看日志"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes("处理日志") && document.querySelector('[role="dialog"]')?.innerText.includes("处理失败")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('[role="dialog"] [data-slot="dialog-close"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="dialog"]')`)
	valid, err := os.ReadFile("internal/engine/testdata/encrypted.sz")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mygoE2E.app.Tasks.Path(jobID, "input.sz"), valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('section[aria-label="视频解密"] button')].find(b => b.textContent.includes("重试失败项")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := mygoE2E.app.Tasks.Task(task.ID)
		if ok && view.Status == "completed" && view.Children[0].Attempt == 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if view, ok := mygoE2E.app.Tasks.Task(task.ID); !ok || view.Status != "completed" || view.Completed != 1 || view.Children[0].Attempt != 2 {
		t.Fatalf("retry did not complete in place: %+v, %v", view, ok)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("成功1·失败0")`)
	chunk, err := mygoE2E.app.JobLogs(jobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	retryLog, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil || !bytes.Contains(retryLog, []byte("第 2 次尝试")) || !bytes.Contains(retryLog, []byte("输入检查失败")) {
		t.Fatalf("retry did not preserve earlier failure and new attempt: %q, %v", retryLog, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('section[aria-label="视频解密"] button')].find(b => b.textContent.includes("返回任务列表")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/decrypt" && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部2")`)
	deleteClick := `(() => {
		const row=[...document.querySelectorAll('section[aria-label="视频解密"] tr')].find(r => r.textContent.includes("E2E retry task"));
		row.querySelector('button[aria-label^="删除任务"]').click();
		return true;
	})()`
	if _, err := mygo.EvalAs[bool](w.Page(), deleteClick); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.textContent.includes("确认删除")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('[role="alertdialog"] button')].find(b => b.textContent.includes("取消")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]') && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部2")`)
	if _, err := mygo.EvalAs[bool](w.Page(), deleteClick); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alertdialog"]')?.textContent.includes("确认删除")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { [...document.querySelectorAll('[role="alertdialog"] button')].find(b => b.textContent.includes("确认删除")).click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alertdialog"]') && document.querySelector('section[aria-label="视频解密"]')?.textContent.replace(/\s+/g,"").includes("全部1")`)
	if _, ok := mygoE2E.app.Tasks.Task(task.ID); ok {
		t.Fatal("deleted task remained")
	}
	var published int
	if err := mygoE2E.db.DB.QueryRow("SELECT COUNT(*) FROM resources WHERE task_id=?", task.ID).Scan(&published); err != nil || published != 1 {
		t.Fatalf("delete removed a published resource: %d, %v", published, err)
	}
}
