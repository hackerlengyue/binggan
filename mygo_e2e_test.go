//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/playerautomation"
	"time.haomen/binggan/v2/internal/store"
)

// This opt-in test runs the real WKWebView with isolated data. The normal Go
// suite stays headless; run this after building frontend/dist and resources.
var mygoE2E *Workbench
var mygoLifecycle *LifecycleService
var mygoE2EStart sync.Once

// The native WebView test uses a fixed catalog and never controls SzPlayer.
type mygoPlayerFixture struct {
	accessibilityBlocked atomic.Bool
	notInstalled         atomic.Bool
	courseFailure        atomic.Bool
	courseEmpty          atomic.Bool
	permissionOpens      atomic.Int32
	resumePending        atomic.Bool
	resumeDeclines       atomic.Int32
}

func (f *mygoPlayerFixture) Status() playerautomation.Status {
	return playerautomation.Status{Platform: "darwin", Installed: !f.notInstalled.Load(), Running: true, AccessibilityReady: !f.accessibilityBlocked.Load(), VerifiedProfile: true, Version: "26.06.54", Message: "test catalog"}
}

func (f *mygoPlayerFixture) CourseSnapshot() (playerautomation.Snapshot, error) {
	if f.courseFailure.Load() {
		return playerautomation.Snapshot{}, fmt.Errorf("isolated catalog unavailable")
	}
	if f.courseEmpty.Load() {
		return playerautomation.Snapshot{Running: true, Elements: []playerautomation.Element{{Role: "AXOutline"}}}, nil
	}
	return playerautomation.Snapshot{Running: true, Elements: []playerautomation.Element{
		{Role: "AXOutline", Depth: 1},
		{Role: "AXRow", Depth: 2, Level: 0, Value: "阶段一", CanExpand: true},
		{Role: "AXRow", Depth: 3, Level: 1, Value: "第一讲"},
		{Role: "AXRow", Depth: 3, Level: 1, Value: "第二讲"},
		{Role: "AXRow", Depth: 2, Level: 0, Value: "阶段二", CanExpand: true},
		{Role: "AXRow", Depth: 3, Level: 1, Value: "第一讲"},
	}}, nil
}

func (f *mygoPlayerFixture) Snapshot() (playerautomation.Snapshot, error) {
	if f.resumePending.Load() {
		return playerautomation.Snapshot{Running: true, Elements: []playerautomation.Element{
			{Role: "AXOutline"},
			{Role: "AXStaticText", Value: "是否继续上次播放的视频位置？[00:02:00]"},
			{Role: "AXButton", Title: "是"}, {Role: "AXButton", Title: "否"},
		}}, nil
	}
	if f.resumeDeclines.Load() > 0 {
		return playerautomation.Snapshot{Running: true, Elements: []playerautomation.Element{{Role: "AXOutline"}}}, nil
	}
	return playerautomation.Snapshot{Running: true}, nil
}
func (f *mygoPlayerFixture) PlaybackSnapshot() (playerautomation.Snapshot, error) {
	return f.Snapshot()
}
func (f *mygoPlayerFixture) RevealCourse(playerautomation.CourseTargetRequest) (playerautomation.Snapshot, error) {
	return playerautomation.Snapshot{}, fmt.Errorf("test fixture cannot expand a folder")
}
func (*mygoPlayerFixture) OpenCourse(string) error {
	return fmt.Errorf("test fixture cannot open a course")
}
func (*mygoPlayerFixture) ExpandFolder(string) error {
	return fmt.Errorf("test fixture cannot expand a folder")
}
func (*mygoPlayerFixture) SetVolume(int) error { return fmt.Errorf("test fixture cannot set volume") }
func (*mygoPlayerFixture) SetFullscreen(bool) error {
	return fmt.Errorf("test fixture cannot set fullscreen")
}
func (*mygoPlayerFixture) SetPlaybackRate(float64) error {
	return fmt.Errorf("test fixture cannot set playback rate")
}
func (*mygoPlayerFixture) TogglePlayback() error {
	return fmt.Errorf("test fixture cannot toggle playback")
}
func (f *mygoPlayerFixture) Click(request playerautomation.ClickRequest) error {
	if request.Text == "否" && request.ClickCount == 1 && f.resumePending.CompareAndSwap(true, false) {
		f.resumeDeclines.Add(1)
		return nil
	}
	return fmt.Errorf("test fixture cannot click")
}
func (f *mygoPlayerFixture) RequestAccessibilityPermission() error {
	f.permissionOpens.Add(1)
	return fmt.Errorf("test fixture cannot open settings")
}

var _ playerautomation.Driver = (*mygoPlayerFixture)(nil)

func TestMain(m *testing.M) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		os.Exit(m.Run())
	}
	root, err := os.MkdirTemp("", "binggan-mygo-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(root, "app"))
	mygo.App.SetPath(mygo.PathResources, filepath.Join(cwd, "resources", "darwin-arm64"))
	// Use independent receiver/proxy ports alongside the installed application.
	for _, variable := range []string{"BINGGAN_RECEIVER_PORT", "CAPTURE_PROXY_PORT"} {
		listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			fmt.Fprintln(os.Stderr, listenErr)
			os.Exit(1)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		_ = os.Setenv(variable, fmt.Sprint(port))
	}
	mygoE2E = newWorkbench()
	mygoLifecycle = newLifecycleService()
	mygo.Bind(mygoLifecycle)
	mygo.Bind(&CaptureService{mygoE2E})
	mygo.Bind(&PlayerService{mygoE2E})
	mygo.Bind(&WorkspaceService{mygoE2E})
	mygo.Bind(&NotificationService{mygoE2E})
	mygo.SetFrontend(os.DirFS(filepath.Join(cwd, "frontend", "dist")))
	if err := mygo.Protocol.Handle("binggan-stream", mygoE2E.streamHandler()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mygo.App.SetMenu(mygoE2E.menu())
	// The suite owns app shutdown; individual tests may close their last window.
	mygo.App.OnWindowAllClosed(func() {})
	code := 1
	mygo.App.WhenReady(func() {
		go func() {
			code = m.Run()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	mygoE2E.close()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func waitMyGoPage(t *testing.T, w *mygo.Window, expression string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		// DOM content behind the startup overlay is not yet an interactive page.
		if ok, _ := mygo.EvalAs[bool](w.Page(), "!document.querySelector('.splash') && !!("+expression+")"); ok {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	state, _ := mygo.EvalAs[map[string]any](w.Page(), `({origin:performance.timeOrigin,grid:localStorage.getItem("bg.resources.view"),card:document.querySelector("article h2")?.textContent,url:location.href, main:document.querySelector('#main-content')?.innerText.slice(0,700), dialog:document.querySelector('[role="dialog"]')?.innerText.slice(0,500), alert:document.querySelector('[role="alert"]')?.innerText.slice(0,300)})`)
	t.Fatalf("timed out waiting for %s; page=%+v", expression, state)
}

func TestMyGoWebView(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("set BINGGAN_MYGO_E2E=1 to run the native WebView test")
	}
	playerFixture := &mygoPlayerFixture{}
	mygoE2E.player = playerFixture
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("workspace startup timed out")
	}
	source := filepath.Join(t.TempDir(), "blue.mp4")
	cmd := exec.Command(filepath.Join("resources", "darwin-arm64", "tools", "ffmpeg"),
		"-hide_banner", "-loglevel", "error", "-nostdin", "-f", "lavfi", "-i", "color=c=blue:s=160x90:d=2",
		"-an", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-movflags", "+faststart", source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("video fixture: %v: %s", err, out)
	}
	resource := store.Resource{ID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", Name: "WebView test.mp4", FinishedAt: store.Now()}
	if err := mygoE2E.db.PublishResource(mygoE2E.app.Config.DataDir, source, resource); err != nil {
		t.Fatal(err)
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Binggan WebView E2E", URL: "/", Width: 1320, Height: 880})
	mygoE2E.window = w
	defer w.Destroy()
	waitMyGoPage(t, w, `document.readyState === "complete" && typeof window.mygo !== "undefined"`)
	// Keep the production notification binding, but suppress OS delivery from
	// this isolated fixture. These event preferences also apply after reloads.
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		for (const kind of ['capture.start', 'capture.saved', 'capture.attention', 'player.start', 'player.courseSaved', 'player.pauseResume', 'player.attention', 'player.ended', 'player.completed', 'decrypt.completed', 'decrypt.attention']) localStorage.setItem('szjm.notifications.' + kind + '.v2', 'false');
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('#main-content section[aria-label="数据概览"]')`)
	previousView, err := mygo.EvalAs[string](w.Page(), `localStorage.getItem("bg.resources.view") || ""`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
			const current=%q;
			if (current) localStorage.setItem("bg.resources.view",current); else localStorage.removeItem("bg.resources.view");
			return true;
		})()`, previousView))
	}()
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('a[aria-label="资源管理"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/resources" && document.querySelector('section[aria-label="资源管理"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="卡片视图"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('article h2')?.textContent === "WebView test.mp4"`)
	waitMyGoPage(t, w, `document.querySelector('article img')?.naturalWidth > 0`)
	remountResources := func() {
		t.Helper()
		if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('a[aria-label="数据概览"]').click(); return true })()`); err != nil {
			t.Fatal(err)
		}
		waitMyGoPage(t, w, `location.hash === "#/overview" && document.querySelector('section[aria-label="数据概览"]')`)
		if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('a[aria-label="资源管理"]').click(); return true })()`); err != nil {
			t.Fatal(err)
		}
		waitMyGoPage(t, w, `location.hash === "#/resources" && document.querySelector('section[aria-label="资源管理"]') && document.querySelector('table, article')`)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		localStorage.removeItem("bg.resources.view");
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	remountResources()
	if grid, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('article') && !document.querySelector('table')`); err != nil || !grid {
		t.Fatalf("missing view preference must default to grid: grid=%v, %v", grid, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		localStorage.setItem("bg.resources.view", "invalid");
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	remountResources()
	if grid, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('article') && !document.querySelector('table')`); err != nil || !grid {
		t.Fatalf("invalid preference must default to grid: grid=%v, %v", grid, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		localStorage.setItem("bg.resources.view", "grid");
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	remountResources()
	if grid, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('article') && !document.querySelector('table')`); err != nil || !grid {
		t.Fatalf("saved grid preference must be restored: grid=%v, %v", grid, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { localStorage.removeItem("bg.resources.view"); return true })()`); err != nil {
		t.Fatal(err)
	}
	remountResources()
	if grid, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('article') && !document.querySelector('table')`); err != nil || !grid {
		t.Fatalf("removed preference must default to grid: grid=%v, %v", grid, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="列表视图"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!!document.querySelector('table') && localStorage.getItem("bg.resources.view") === "list"`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="卡片视图"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `localStorage.getItem("bg.resources.view") === "grid" && document.querySelector('article h2')?.textContent === "WebView test.mp4"`)
	beforeReload, err := mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.reload(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin > %.0f && document.readyState === "complete" && location.hash === "#/resources" && document.querySelector('article h2')?.textContent === "WebView test.mp4"`, beforeReload))
	if grid, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('article') && !document.querySelector('table') && localStorage.getItem("bg.resources.view") === "grid"`); err != nil || !grid {
		t.Fatalf("explicit grid preference must persist across reload: grid=%v, %v", grid, err)
	}
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-resources.png", png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	partial, err := mygo.EvalAs[struct {
		Status int `json:"status"`
		Bytes  int `json:"bytes"`
	}](w.Page(), `(async () => {
		const response = await fetch("binggan-stream://localhost/api/resources/eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee/stream", {headers:{Range:"bytes=0-15"}});
		return {status:response.status, bytes:(await response.arrayBuffer()).byteLength};
	})()`)
	if err != nil || partial.Status != 206 || partial.Bytes != 16 {
		t.Fatalf("WebView Range = %+v, %v", partial, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('article button[aria-label^="播放视频"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('iframe[title="视频播放器"]')?.contentDocument?.querySelector('video')?.readyState >= 2`)
	video, err := mygo.EvalAs[struct {
		Source   string  `json:"source"`
		Duration float64 `json:"duration"`
		Width    int     `json:"width"`
		Error    bool    `json:"error"`
	}](w.Page(), `(() => {
		const video = document.querySelector('iframe[title="视频播放器"]').contentDocument.querySelector('video');
		return {source:video.currentSrc, duration:video.duration, width:video.videoWidth, error:!!video.error};
	})()`)
	if err != nil || video.Error || video.Duration < 1.5 || video.Width != 160 || !strings.HasPrefix(video.Source, "binggan-stream://localhost/") {
		t.Fatalf("WebView video = %+v, %v", video, err)
	}
	seeked, err := mygo.EvalAs[float64](w.Page(), `(async () => {
		const video = document.querySelector('iframe[title="视频播放器"]').contentDocument.querySelector('video');
		video.pause();
		video.currentTime = 1;
		await new Promise((resolve, reject) => {
			if (!video.seeking && Math.abs(video.currentTime - 1) < 0.1) return resolve();
			video.addEventListener('seeked', resolve, {once:true});
			setTimeout(() => reject(new Error('seek timed out')), 5000);
		});
		return video.currentTime;
	})()`)
	if err != nil || seeked < 0.9 || seeked > 1.1 {
		t.Fatalf("WebView seek time = %v, %v", seeked, err)
	}
	volume, err := mygo.EvalAs[struct {
		AfterAdjustMuted     bool    `json:"afterAdjustMuted"`
		AfterAdjustVolume    float64 `json:"afterAdjustVolume"`
		AfterRoundTripMuted  bool    `json:"afterRoundTripMuted"`
		AfterRoundTripVolume float64 `json:"afterRoundTripVolume"`
	}](w.Page(), `(() => {
		const frame = document.querySelector('iframe[title="视频播放器"]').contentDocument;
		const video = frame.querySelector('video');
		const mute = frame.querySelector('[data-act="mute"]');
		const slider = frame.querySelector('[data-el="vol"]');
		video.volume = 1;
		mute.click();
		slider.value = '0.44';
		slider.dispatchEvent(new Event('input', {bubbles:true}));
		const afterAdjust = {muted:video.muted, volume:video.volume};
		mute.click();
		mute.click();
		return {afterAdjustMuted:afterAdjust.muted, afterAdjustVolume:afterAdjust.volume, afterRoundTripMuted:video.muted, afterRoundTripVolume:video.volume};
	})()`)
	if err != nil || volume.AfterAdjustMuted || math.Abs(volume.AfterAdjustVolume-0.44) > 0.01 || volume.AfterRoundTripMuted || math.Abs(volume.AfterRoundTripVolume-0.44) > 0.01 {
		t.Fatalf("WebView volume after mute, adjust and unmute = %+v, %v", volume, err)
	}
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-player.png", png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		window.__closedVideo = document.querySelector('iframe[title="视频播放器"]').contentDocument.querySelector('video');
		document.querySelector('[role="dialog"] [data-slot="dialog-close"]').click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('iframe[title="视频播放器"]') && window.__closedVideo?.paused && !window.__closedVideo?.hasAttribute('src')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { delete window.__closedVideo; return true })()`); err != nil {
		t.Fatal(err)
	}
	downloadDir := t.TempDir()
	type downloadStart struct{ name, path string }
	started := make(chan downloadStart, 2)
	chosen := make(chan string, 2)
	done := make(chan *mygo.Download, 2)
	reported := make(chan error, 2)
	downloadCount := 0
	manager := installDownloads(w)
	defer manager.close()
	manager.choose = func(opts mygo.SaveDialogOptions) (string, error) {
		if opts.Title != "保存文件" || filepath.Base(opts.DefaultPath) != resource.Name {
			return "", fmt.Errorf("unexpected native save options: %+v", opts)
		}
		downloadCount++
		dest := filepath.Join(downloadDir, fmt.Sprintf("%d-%s", downloadCount, resource.Name))
		chosen <- dest
		return dest, nil
	}
	manager.report = func(err error) { reported <- err }
	w.Page().OnWillDownload(func(event *mygo.DownloadEvent) {
		started <- downloadStart{event.SuggestedName, event.Path}
	})
	w.Page().OnDownloadDone(func(download *mygo.Download) { done <- download })
	checkDownload := func(label string) {
		t.Helper()
		var start downloadStart
		select {
		case start = <-started:
			if start.name != resource.Name {
				t.Errorf("%s suggested name = %q", label, start.name)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s attachment did not start", label)
		}
		var dest string
		select {
		case dest = <-chosen:
			if start.path == dest || !strings.HasPrefix(filepath.Base(start.path), ".binggan-download-") {
				t.Fatalf("%s did not stage the download: %q → %q", label, start.path, dest)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s native destination was not chosen", label)
		}
		select {
		case download := <-done:
			if download.Err != nil {
				t.Fatal(download.Err)
			}
			if download.Path != start.path {
				t.Fatalf("%s completed unexpected stage: %q", label, download.Path)
			}
			gotBytes, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(start.path); !os.IsNotExist(err) {
				t.Fatalf("%s left a staging file: %v", label, err)
			}
			wantBytes, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(gotBytes, wantBytes) {
				t.Fatalf("%s download bytes differ: %d vs %d, %v", label, len(gotBytes), len(wantBytes), err)
			}
			select {
			case err := <-reported:
				t.Fatalf("%s native download failed: %v", label, err)
			default:
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s attachment did not finish", label)
		}
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('article a[aria-label^="下载视频"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	checkDownload("grid")
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="列表视图"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('table tbody tr')?.innerText.includes("WebView test.mp4") && localStorage.getItem("bg.resources.view") === "list"`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('table a[aria-label^="下载视频"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	checkDownload("list")
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('table button[aria-label^="播放视频"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('iframe[title="视频播放器"]')?.contentDocument?.querySelector('video')?.readyState >= 2`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('[role="dialog"] [data-slot="dialog-close"]').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { const input=document.querySelector('input[aria-label="搜索资源"]'); input.value="not-in-library"; input.dispatchEvent(new Event('input',{bubbles:true})); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="资源管理"]')?.innerText.includes("没有匹配的视频")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { const input=document.querySelector('input[aria-label="搜索资源"]'); input.value=""; input.dispatchEvent(new Event('input',{bubbles:true})); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('table tbody tr')?.innerText.includes("WebView test.mp4")`)
	missing, err := mygo.EvalAs[int](w.Page(), `(async () => (await fetch("binggan-stream://localhost/api/resources/missing/stream")).status)()`)
	if err != nil || missing != 404 {
		t.Fatalf("missing WebView resource = %d, %v", missing, err)
	}
	routes := []struct{ title, path, label string }{
		{"秘钥提取", "/capture", "本次秘钥提取"},
		{"秘钥管理", "/history", "秘钥管理"},
		{"播放编排", "/player-orchestration", "播放编排"},
		{"视频解密", "/decrypt", "视频解密"},
		{"证书管理", "/certificates", "证书管理"},
		{"环境信息", "/environment", "环境信息"},
		{"解密设置", "/decrypt-settings", "解密设置"},
		{"日志信息", "/logs", "日志查询"},
		{"资源管理", "/resources", "资源管理"},
		{"数据概览", "/overview", "数据概览"},
	}
	for _, route := range routes {
		js := fmt.Sprintf(`(() => { const link=[...document.querySelectorAll('a[aria-label]')].find(a=>a.getAttribute('aria-label')===%q); if (!link) throw Error('missing navigation'); link.click(); return true })()`, route.title)
		if _, err := mygo.EvalAs[bool](w.Page(), js); err != nil {
			t.Fatalf("navigate %s: %v", route.path, err)
		}
		section := fmt.Sprintf(`location.hash === %q && document.querySelector('#main-content section[aria-label=%q]') && (() => { const active=[...document.querySelectorAll('.workspace-nav-item[aria-current="page"]')]; return active.length === 1 && active[0].getAttribute('aria-label') === %q })()`, "#"+route.path, route.label, route.title)
		waitMyGoPage(t, w, section)
		width, err := mygo.EvalAs[struct {
			View   int `json:"view"`
			Scroll int `json:"scroll"`
		}](w.Page(), `({view:innerWidth, scroll:document.documentElement.scrollWidth})`)
		if err != nil || width.Scroll > width.View+1 {
			t.Fatalf("%s horizontal overflow: %+v, %v", route.path, width, err)
		}
		if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
			time.Sleep(250 * time.Millisecond) // let the sidebar color transition finish before a snapshot
			png, err := w.CapturePage()
			if err != nil {
				t.Fatal(err)
			}
			name := strings.TrimPrefix(route.path, "/")
			if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-"+name+".png", png, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, path := range []string{"/resources", "/logs"} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => { location.hash=%q; return true })()`, "#"+path)); err != nil {
			t.Fatal(err)
		}
		waitMyGoPage(t, w, fmt.Sprintf(`location.hash === %q`, "#"+path))
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { history.back(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/resources" && document.querySelector('#main-content section[aria-label="资源管理"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.reload(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.readyState === "complete" && location.hash === "#/resources" && document.querySelector('#main-content section[aria-label="资源管理"]')`)
	for _, alias := range []struct{ source, target, label, title string }{
		{"/unknown-workspace", "/overview", "数据概览", "数据概览"},
	} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => { location.hash=%q; return true })()`, "#"+alias.source)); err != nil {
			t.Fatalf("navigate unknown route %s: %v", alias.source, err)
		}
		waitMyGoPage(t, w, fmt.Sprintf(`location.hash === %q && document.querySelector('#main-content section[aria-label=%q]') && (() => { const active=[...document.querySelectorAll('.workspace-nav-item[aria-current="page"]')]; return active.length === 1 && active[0].getAttribute('aria-label') === %q })()`, "#"+alias.target, alias.label, alias.title))
	}
	for _, size := range [][2]int{{1000, 680}, {390, 844}} {
		w.SetSize(size[0], size[1])
		waitMyGoPage(t, w, fmt.Sprintf(`innerWidth <= %d`, size[0]+2))
		width, err := mygo.EvalAs[struct {
			View   int `json:"view"`
			Scroll int `json:"scroll"`
		}](w.Page(), `({view:innerWidth, scroll:document.documentElement.scrollWidth})`)
		if err != nil || width.Scroll > width.View+1 {
			t.Fatalf("overview at %dx%d overflows: %+v, %v", size[0], size[1], width, err)
		}
		if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
			png, err := w.CapturePage()
			if err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("-%dx%d.png", size[0], size[1])
			if err := os.WriteFile(strings.TrimSuffix(out, ".png")+name, png, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.hash="#/resources"; return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `location.hash === "#/resources" && document.querySelector('section[aria-label="资源管理"]')`)
	for _, view := range []struct{ name, content string }{
		{"列表视图", "table"},
		{"卡片视图", "article"},
	} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => { document.querySelector('button[aria-label=%q]').click(); return true })()`, view.name)); err != nil {
			t.Fatal(err)
		}
		waitMyGoPage(t, w, fmt.Sprintf(`document.querySelector(%q)`, view.content))
		width, err := mygo.EvalAs[struct {
			View   int `json:"view"`
			Scroll int `json:"scroll"`
		}](w.Page(), `({view:innerWidth, scroll:document.documentElement.scrollWidth})`)
		if err != nil || width.Scroll > width.View+1 {
			t.Fatalf("resource %s at 390px overflows: %+v, %v", view.name, width, err)
		}
	}
	newResource := store.Resource{ID: "dddddddd-dddd-dddd-dddd-dddddddddddd", Name: "Refreshed resource.mp4", FinishedAt: store.Now()}
	if err := mygoE2E.db.PublishResource(mygoE2E.app.Config.DataDir, source, newResource); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="资源管理"]')?.innerText.includes("共 2 条") && [...document.querySelectorAll('article h2')].some(h => h.textContent === "Refreshed resource.mp4")`)
	if _, err := mygoE2E.db.DB.Exec("ALTER TABLE resources RENAME TO resources_test_hidden"); err != nil {
		t.Fatal(err)
	}
	resourcesRestored := false
	defer func() {
		if !resourcesRestored {
			_, _ = mygoE2E.db.DB.Exec("ALTER TABLE resources_test_hidden RENAME TO resources")
		}
	}()
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input = document.querySelector('input[aria-label="搜索资源"]');
		input.value = "missing-while-failed";
		input.dispatchEvent(new Event('input', {bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="alert"]')?.innerText.includes("资源加载失败") && !document.querySelector('article')`)
	if _, err := mygoE2E.db.DB.Exec("ALTER TABLE resources_test_hidden RENAME TO resources"); err != nil {
		t.Fatal(err)
	}
	resourcesRestored = true
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('[role="alert"] button').click(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="alert"]') && document.querySelector('section[aria-label="资源管理"]')?.innerText.includes("没有匹配的视频")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input = document.querySelector('input[aria-label="搜索资源"]');
		input.value = "";
		input.dispatchEvent(new Event('input', {bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="资源管理"]')?.innerText.includes("共 2 条") && document.querySelectorAll('article').length === 2`)
	brokenSource := filepath.Join(t.TempDir(), "broken.mp4")
	if err := os.WriteFile(brokenSource, []byte("not an MP4 video"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := store.Resource{ID: "cccccccc-cccc-cccc-cccc-cccccccccccc", Name: "Broken video.mp4", FinishedAt: store.Now()}
	if err := mygoE2E.db.PublishResource(mygoE2E.app.Config.DataDir, brokenSource, broken); err != nil {
		t.Fatal(err)
	}
	missingResource := store.Resource{ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Name: "Missing video.mp4", FinishedAt: store.Now()}
	if err := mygoE2E.db.PublishResource(mygoE2E.app.Config.DataDir, source, missingResource); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.ResourcePath(mygoE2E.app.Config.DataDir, missingResource.ID)); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="资源管理"]')?.innerText.includes("共 4 条") && document.querySelectorAll('article').length === 4`)
	for _, name := range []string{broken.Name, missingResource.Name} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
			const article = [...document.querySelectorAll('article')].find(a => a.querySelector('h2')?.textContent === %q);
			if (!article) throw Error('resource card missing');
			article.querySelector('button[aria-label^="播放视频"]').click();
			return true;
		})()`, name)); err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		waitMyGoPage(t, w, `document.querySelector('iframe[title="视频播放器"]')?.contentDocument?.querySelector('#error:not([hidden])')?.textContent.includes("无法播放此视频")`)
		if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('[role="dialog"] [data-slot="dialog-close"]').click(); return true })()`); err != nil {
			t.Fatalf("close %s: %v", name, err)
		}
		waitMyGoPage(t, w, `!document.querySelector('iframe[title="视频播放器"]')`)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const article = [...document.querySelectorAll('article')].find(a => a.querySelector('h2')?.textContent === "Missing video.mp4");
		article.querySelector('a[aria-label^="下载视频"]').click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-done:
		if outcome.Err == nil || outcome.Path != "" {
			t.Fatalf("missing attachment unexpectedly downloaded: %+v", outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("missing attachment did not report a download error")
	}
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("missing attachment reported a nil error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("missing attachment did not report the failure to the user")
	}
	select {
	case <-started:
		t.Fatal("missing attachment unexpectedly opened a save dialog")
	default:
	}
	page, err := mygo.EvalAs[bool](w.Page(), `location.hash === "#/resources" && !!document.querySelector('section[aria-label="资源管理"]')`)
	if err != nil || !page {
		t.Fatalf("missing attachment left the resources page: %v, %v", page, err)
	}
	verifyMyGoDecryptFlow(t, w)
	verifyMyGoRetryAndDelete(t, w)
	verifyMyGoVideoImportChannel(t, w)
	verifyMyGoPlayerPlanner(t, w)
	verifyMyGoPlayerReadiness(t, w, playerFixture)
	verifyMyGoLogClear(t, w)
	verifyMyGoHistory(t, w, manager, done)
	verifyMyGoCertificatePage(t, w)
	verifyMyGoConnectionDescriptor(t, w)
	verifyMyGoUntrustedMonitor(t, w)
	verifyMyGoCaptureExport(t, w, manager, done)
}

func TestMyGoQuitHandshake(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("native WebView opt-in")
	}
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("startup timed out")
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Binggan Quit E2E", URL: "/", Width: 1320, Height: 880})
	defer w.Destroy()
	waitMyGoPage(t, w, `document.querySelector('#main-content section[aria-label="数据概览"]')`)
	failures := make(chan string, 1)
	approved := make(chan struct{}, 1)
	mygoLifecycle.report = func(message string, _ func()) { failures <- message }
	mygoLifecycle.quit = func() { approved <- struct{}{} }
	_, err := mygo.EvalAs[bool](w.Page(), `(() => { window.savedSetItem = Storage.prototype.setItem; Storage.prototype.setItem = function() { throw new Error('fixture disk full'); }; return true; })()`)
	if err != nil {
		t.Fatal(err)
	}
	if mygoLifecycle.allowQuit() {
		t.Fatal("quit was not intercepted")
	}
	select {
	case message := <-failures:
		if !strings.Contains(message, "任务状态保存失败") {
			t.Fatalf("wrong failure: %s", message)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("renderer did not report failed journal")
	}
	select {
	case <-approved:
		t.Fatal("failed save approved quit")
	default:
	}
	_, err = mygo.EvalAs[bool](w.Page(), `(() => { Storage.prototype.setItem = window.savedSetItem; return true; })()`)
	if err != nil {
		t.Fatal(err)
	}
	// The warning callback is asynchronous; wait for the request to be released.
	deadline := time.Now().Add(time.Second)
	for {
		mygoLifecycle.mu.Lock()
		pending := mygoLifecycle.pending
		mygoLifecycle.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failure did not release pending request")
		}
		time.Sleep(time.Millisecond)
	}
	if mygoLifecycle.allowQuit() {
		t.Fatal("retry skipped renderer")
	}
	select {
	case <-approved:
	case <-time.After(10 * time.Second):
		t.Fatal("saved renderer did not approve quit")
	}
}

func TestMyGoCourseCatalogStates(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("native WebView opt-in")
	}
	fixture := &mygoPlayerFixture{}
	mygoE2E.player = fixture
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("startup timed out")
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Binggan Catalog E2E", URL: "/", Width: 1320, Height: 880})
	defer w.Destroy()
	waitMyGoPage(t, w, `document.querySelector('#main-content section[aria-label="数据概览"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { localStorage.removeItem('szjm.player-orchestration.v1'); location.hash = '#/player-orchestration'; return true; })()`); err != nil {
		t.Fatal(err)
	}
	clickText := func(label string) {
		t.Helper()
		expression := fmt.Sprintf(`(() => { const b = [...document.querySelectorAll('button')].find(b => b.textContent.trim() === %q); if (!b || b.disabled) return false; b.click(); return true; })()`, label)
		if ok, err := mygo.EvalAs[bool](w.Page(), expression); err != nil || !ok {
			t.Fatalf("cannot click %s: %v", label, err)
		}
	}
	waitMyGoPage(t, w, `[...document.querySelectorAll('button')].some(b => b.textContent.trim() === '下一步' && !b.disabled)`)
	clickText("下一步")
	waitMyGoPage(t, w, `document.querySelector('input[aria-label="搜索课程"]') && document.querySelector('#main-content').innerText.includes('已选择 3 / 3 节')`)
	// Search emptiness must not erase the underlying catalog or selected count.
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { const input = document.querySelector('input[aria-label="搜索课程"]'); input.value = '不存在的课程'; input.dispatchEvent(new Event('input', {bubbles:true})); return true; })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('#main-content').innerText.includes('没有匹配的课程') && document.querySelector('#main-content').innerText.includes('已选择 3 / 3 节')`)
	clickText("清空搜索")
	waitMyGoPage(t, w, `document.querySelectorAll('[role="checkbox"]').length > 0`)
	fixture.courseEmpty.Store(true)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { document.querySelector('button[aria-label="重新获取课程"]').click(); return true; })()`); err != nil {
		t.Fatal(err)
	}
	emptyCheck := `document.querySelector('#main-content').innerText.includes('暂无课程') && !document.querySelector('#main-content').innerText.includes('课程读取失败') && !document.querySelector('input[aria-label="搜索课程"]') && !document.querySelector('[role="checkbox"]') && [...document.querySelectorAll('button')].some(b => b.textContent.trim() === '下一步' && b.disabled)`
	waitMyGoPage(t, w, emptyCheck)
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.courseFailure.Store(true)
	clickText("重新获取课程")
	waitMyGoPage(t, w, `document.querySelector('#main-content').innerText.includes('课程读取失败') && document.querySelector('#main-content').innerText.includes('isolated catalog unavailable') && !document.querySelector('[role="checkbox"]')`)
	fixture.courseFailure.Store(false)
	fixture.courseEmpty.Store(false)
	clickText("重试读取")
	waitMyGoPage(t, w, `document.querySelector('input[aria-label="搜索课程"]') && document.querySelector('#main-content').innerText.includes('已选择 0 / 3 节') && !document.querySelector('#main-content').innerText.includes('课程读取失败')`)
	// Persisted selection/cache cannot restore deleted courses after reopening.
	fixture.courseEmpty.Store(true)
	_, err := mygo.EvalAs[bool](w.Page(), `(() => { location.reload(); return true; })()`)
	if err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('#main-content').innerText.includes('运行条件') && [...document.querySelectorAll('button')].some(b => b.textContent.trim() === '下一步' && !b.disabled)`)
	clickText("下一步")
	waitMyGoPage(t, w, emptyCheck)
}

func TestMyGoTrayKeepsWindowAliveAndExplicitQuitCloses(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("native WebView opt-in")
	}
	mygoLifecycle.mu.Lock()
	mygoLifecycle.approved = false
	mygoLifecycle.mu.Unlock()
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("startup timed out")
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Binggan Tray E2E", URL: "/", Width: 1320, Height: 880})
	defer w.Destroy()
	cleanup := installWindowTray(w, mygoLifecycle)
	defer cleanup()
	mygoLifecycle.reveal = func() { showMainWindow(w) }
	waitMyGoPage(t, w, `document.querySelector('#main-content section[aria-label="数据概览"]')`)
	origin, err := mygo.EvalAs[float64](w.Page(), `(() => { localStorage.removeItem('szjm.player-orchestration.v1'); window.trayTicks = 0; setInterval(() => window.trayTicks++, 200); return performance.timeOrigin; })()`)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if w.IsDestroyed() || w.IsVisible() || mygoLifecycle.windowMayClose() {
		t.Fatal("close destroyed or kept the window visible")
	}
	time.Sleep(1200 * time.Millisecond)
	if ticks, err := mygo.EvalAs[int](w.Page(), `window.trayTicks`); err != nil || ticks < 1 {
		t.Fatalf("hidden renderer timer stopped: %d, %v", ticks, err)
	}
	if mygoE2E.ctx.Err() != nil {
		t.Fatal("hiding cancelled workbench")
	}
	showMainWindow(w)
	if !w.IsVisible() {
		t.Fatal("hidden window did not reopen")
	}
	if same, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`performance.timeOrigin === %.0f && window.trayTicks > 0`, origin)); err != nil || !same {
		t.Fatalf("restoring recreated page: %v %v", same, err)
	}
	menu := windowTrayMenu(w)
	menu.ItemByID("hide-window").Click(nil, nil)
	if w.IsVisible() || w.IsDestroyed() {
		t.Fatal("tray hide action failed")
	}
	menu.ItemByID("show-window").Click(nil, nil)
	if !w.IsVisible() {
		t.Fatal("tray show action failed")
	}
	if menu.ItemByID("quit").Role != mygo.RoleQuit {
		t.Fatal("tray quit bypasses normal app quit")
	}
	w.Minimize()
	deadline := time.Now().Add(3 * time.Second)
	for !w.IsMinimized() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !w.IsMinimized() {
		t.Fatal("native minimize did not happen")
	}
	showMainWindow(w)
	deadline = time.Now().Add(3 * time.Second)
	for w.IsMinimized() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !w.IsVisible() || w.IsMinimized() {
		t.Fatal("minimized window did not restore")
	}
	approved := make(chan struct{}, 1)
	failed := make(chan bool, 1)
	mygoLifecycle.quit = func() { approved <- struct{}{} }
	mygoLifecycle.report = func(string, func()) { failed <- w.IsVisible() }
	_, err = mygo.EvalAs[bool](w.Page(), `(() => { window.savedSetItem = Storage.prototype.setItem; Storage.prototype.setItem = function() { throw new Error('tray fixture disk full'); }; return true; })()`)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if mygoLifecycle.allowQuit() {
		t.Fatal("hidden quit skipped cleanup")
	}
	select {
	case visible := <-failed:
		if !visible {
			t.Fatal("failed hidden quit did not reveal window")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hidden renderer did not acknowledge failed quit")
	}
	if w.IsDestroyed() || mygoLifecycle.windowMayClose() {
		t.Fatal("failed quit destroyed window")
	}
	_, err = mygo.EvalAs[bool](w.Page(), `(() => { Storage.prototype.setItem = window.savedSetItem; return true; })()`)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for {
		mygoLifecycle.mu.Lock()
		pending := mygoLifecycle.pending
		mygoLifecycle.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed quit remained pending")
		}
		time.Sleep(time.Millisecond)
	}
	w.Close()
	if mygoLifecycle.allowQuit() {
		t.Fatal("retry skipped hidden renderer")
	}
	select {
	case <-approved:
	case <-time.After(10 * time.Second):
		t.Fatal("hidden renderer did not finish quit")
	}
	w.Close()
	if !w.IsDestroyed() {
		t.Fatal("approved explicit quit was intercepted as hide")
	}
}
