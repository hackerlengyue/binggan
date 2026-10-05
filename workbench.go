package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/collectorhttp"
	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/playerautomation"
	"time.haomen/binggan/v2/internal/power"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/stream"
	"time.haomen/binggan/v2/internal/tasks"
	"time.haomen/binggan/v2/internal/workspace"
)

type Workbench struct {
	ctx           context.Context
	cancel        context.CancelFunc
	ready         chan struct{}
	started       bool
	root          string
	apiURL        string
	window        *mygo.Window
	downloads     *downloadManager
	app           *workspace.App
	media         *stream.Handler
	db            *store.Store
	server        *http.Server
	unlock        func() error
	logFile       *os.File
	monitor       *Monitor
	player        playerautomation.Driver
	permissionMu  sync.Mutex
	permissionCmd *exec.Cmd
	startErr      error
	once          sync.Once
	awakeMu       sync.Mutex
	releaseAwake  func()
	power         *power.Controller
	bootMu        sync.RWMutex
	boot          BootstrapStatus
}

type BootstrapStatus struct {
	Phase string `json:"phase"`
	Ready bool   `json:"ready"`
	Error string `json:"error"`
}

func newWorkbench() *Workbench {
	ctx, cancel := context.WithCancel(context.Background())
	return &Workbench{ctx: ctx, cancel: cancel, ready: make(chan struct{}), player: playerautomation.New(), boot: BootstrapStatus{Phase: "正在启动服务"}}
}

// wait blocks until startup finishes or the calling page cancels the request,
// so startup does not strand one goroutine per pending call.
func (a *Workbench) wait(ctx context.Context) error {
	select {
	case <-a.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// keepAwake holds the system awake while decrypt jobs run, so a long batch is
// not interrupted by idle sleep. The display may still turn off.
func (a *Workbench) keepAwake(active bool) {
	a.awakeMu.Lock()
	defer a.awakeMu.Unlock()
	if !active {
		if a.releaseAwake != nil {
			a.releaseAwake()
			a.releaseAwake = nil
		}
		return
	}
	if a.releaseAwake == nil && a.ctx.Err() == nil {
		a.releaseAwake = mygo.Power.KeepAwake("正在解密视频", false)
	}
}

func (a *Workbench) bootstrapStatus() BootstrapStatus {
	a.bootMu.RLock()
	defer a.bootMu.RUnlock()
	return a.boot
}

func (a *Workbench) setBootstrap(status BootstrapStatus) {
	a.bootMu.Lock()
	a.boot = status
	a.bootMu.Unlock()
}

func (a *Workbench) start() {
	defer func() {
		if a.startErr != nil {
			a.setBootstrap(BootstrapStatus{Phase: "启动失败", Error: a.startErr.Error()})
		} else {
			a.setBootstrap(BootstrapStatus{Phase: "完成", Ready: true})
		}
		close(a.ready)
		if a.startErr == nil {
			mygo.RunOnMain(func() {
				if a.ctx.Err() != nil {
					return
				}

				if err := a.power.Restore(); err != nil {
					slog.Error("恢复电源设置失败", "error", err)
				}
			})
		}
	}()
	if err := a.open(); err != nil {
		a.startErr = err
		slog.Error("工作台启动失败", "error", err)
		return
	}
	a.monitor = newMonitor()
	a.monitor.dataDir = filepath.Join(a.root, "capture")
	a.monitor.config = capture.Config{ReceiverURL: a.apiURL, Token: a.app.Config.Token}
	a.monitor.logSink = captureLogSink(a.db)
	a.app.SetLogClearer(a.monitor.clearLogbook)
	a.monitor.startup(a.ctx)
}

func (a *Workbench) open() error {
	root, err := dataRoot()
	if err != nil {
		return err
	}
	a.root = root
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	a.setBootstrap(BootstrapStatus{Phase: "正在启动服务"})
	a.logFile, err = os.OpenFile(filepath.Join(root, "desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(a.logFile, nil)))
	resourceDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	toolDir := filepath.Join(resourceDir, "tools")
	if mygo.IsDev() {
		platformDir := filepath.Join(resourceDir, runtime.GOOS+"-"+runtime.GOARCH, "tools")
		if _, err := os.Stat(toolDir); os.IsNotExist(err) {
			toolDir = platformDir
		}
	}
	tools := []string{"ffmpeg", "ffprobe"}
	if runtime.GOOS == "darwin" {
		// Check the bundled source, not a previously installed launchd helper.
		tools = append(tools, "capture-service")
	}
	for _, tool := range tools {
		name := tool
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		info, err := os.Stat(filepath.Join(toolDir, name))
		if err != nil || !info.Mode().IsRegular() || runtime.GOOS == "darwin" && info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("安装文件不完整，缺少 %s，请重新安装软件", tool)
		}
	}
	if err := os.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return err
	}
	token, err := newCollectorToken()
	if err != nil {
		return fmt.Errorf("无法生成本机采集凭据：%w", err)
	}
	cfg := config.Config{Root: root, DataDir: filepath.Join(root, "data"), Host: "127.0.0.1", Port: receiverPort(), MaxUploadBytes: 20 << 30, Token: token, AppMD5: os.Getenv("SZJM_APP_MD5")}
	if err := workspace.EnsureDirectories(root); err != nil {
		return err
	}
	a.unlock, err = store.Lock(cfg.DataDir)
	if err != nil {
		return err
	}
	a.db, err = store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	a.power = power.New(a.db, func() func() { return mygo.Power.KeepAwake("运行时阻止息屏和屏保", true) })
	a.app, err = workspace.New(cfg, a.db)
	if err != nil {
		return err
	}
	a.app.Tasks.SetActivity(a.keepAwake)
	a.app.Tasks.SetTaskFinished(func(result tasks.Finished) {
		if a.ctx.Err() != nil || a.window == nil {
			return
		}
		if err := DecryptTaskFinished.Emit(a.window, result); err != nil {
			slog.Warn("解密任务通知事件发送失败", "error", err)
		}
	})
	a.media = stream.New(a.ctx, a.db, cfg.DataDir, a.app)
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		return fmt.Errorf("无法启动工作台：端口 %d 被占用：%w", cfg.Port, err)
	}
	if os.Getenv("CAPTURE_PROXY_PORT") == "" {
		os.Setenv("CAPTURE_PROXY_PORT", "18779")
	}
	if err := a.app.StartCollector(); err != nil {
		slog.Warn("抓包入口启动失败", "error", err)
	}
	a.apiURL = "http://" + listener.Addr().String()
	a.server = &http.Server{Handler: collectorhttp.New(a.app, token), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := a.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("桌面服务退出", "error", err)
		}
	}()
	return nil
}

// receiverPort is the fixed loopback port of the receiver. BINGGAN_RECEIVER_PORT
// lets a second build (dev run, isolated WebView test) start beside an installed app.
func receiverPort() int {
	if v := os.Getenv("BINGGAN_RECEIVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 && port < 65536 {
			return port
		}
	}
	return 18778
}

func newCollectorToken() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return hex.EncodeToString(secret), nil
}

func dataRoot() (string, error) {
	return mygo.App.Path(mygo.PathUserData)
}

func (a *Workbench) streamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		allowedOrigin := origin == "mygo://localhost" || origin == "http://mygo.localhost" ||
			mygo.IsDev() && origin == "http://127.0.0.1:5175"
		if origin != "" && !allowedOrigin {
			http.Error(w, "请求来源不允许", http.StatusForbidden)
			return
		}
		if allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Range")
		}
		if r.Method == http.MethodOptions {
			if !allowedOrigin {
				http.Error(w, "请求来源不允许", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		<-a.ready
		if a.startErr != nil || a.app == nil {
			http.Error(w, "工作台尚未就绪", http.StatusServiceUnavailable)
			return
		}
		a.media.ServeHTTP(w, r)
	})
}

func (a *Workbench) close() {
	a.once.Do(func() {
		a.cancel()
		if a.started {
			<-a.ready
		}
		if a.power != nil {
			a.power.Close()
		}
		if a.downloads != nil {
			a.downloads.close()
		}
		if a.monitor != nil {
			a.monitor.beforeClose(context.Background())
			a.monitor.shutdown(context.Background())
		}
		if a.app != nil {
			a.app.Close()
		}
		a.keepAwake(false)
		if a.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if a.server.Shutdown(ctx) != nil {
				_ = a.server.Close()
			}
			cancel()
		}
		if a.db != nil {
			_ = a.db.Close()
		}
		if a.unlock != nil {
			_ = a.unlock()
		}
		if a.logFile != nil {
			_ = a.logFile.Close()
		}
	})
}
