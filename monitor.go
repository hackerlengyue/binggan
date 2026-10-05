package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/tunnel"
)

type State struct {
	Stage           string          `json:"stage"`
	Message         string          `json:"message"`
	ProxyAddress    string          `json:"proxyAddress"`
	ReceiverURL     string          `json:"receiverUrl"`
	TokenConfigured bool            `json:"tokenConfigured"`
	CaptureEnabled  bool            `json:"captureEnabled"`
	Received        int64           `json:"received"`
	Logs            []capture.Entry `json:"logs"`
}
type captureLease interface {
	State() (string, string)
	Stop() error
}

type Monitor struct {
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.RWMutex
	op               sync.Mutex
	certificateOp    sync.Mutex
	state            State
	logs             *capture.Logbook
	config           capture.Config
	dir              string
	lock             *flock.Flock
	lease            captureLease
	startTunnel      func(context.Context, capture.Descriptor, *capture.Logbook) (captureLease, error)
	instance         string
	cursor           int64
	blocked          bool
	runtimeReady     bool
	checkCertificate func(context.Context, capture.Descriptor) (tunnel.CertificateState, error)
	closed           bool
	client           *http.Client
	wake             chan struct{}
	done             chan struct{}
	uiReady          chan struct{}
	uiOnce           sync.Once
	dataDir          string
	logSink          func([]capture.Entry) error
}

func newMonitor() *Monitor {
	return &Monitor{state: State{Stage: "initializing", Message: "正在初始化本地环境", ReceiverURL: capture.DefaultConfig().ReceiverURL, Logs: []capture.Entry{}}, wake: make(chan struct{}, 1), done: make(chan struct{}), uiReady: make(chan struct{}), checkCertificate: tunnel.CertificateStatus, startTunnel: func(ctx context.Context, d capture.Descriptor, logs *capture.Logbook) (captureLease, error) {
		lease, err := tunnel.Start(ctx, d, logs)
		if err != nil {
			return nil, err
		}
		return lease, nil
	}}
}
func (a *Monitor) set(stage, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := a.state.Stage != stage || a.state.Message != message
	a.state.Stage = stage
	a.state.Message = message
	if changed && message != "" && a.logs != nil {
		level := "info"
		if stage == "error" {
			level = "error"
		} else if stage == "offline" || stage == "certificate" || stage == "trust" {
			level = "warn"
		}
		a.logs.AddStatus(level, message)
	}
}
func (a *Monitor) startup(ctx context.Context) { a.ctx, a.cancel = context.WithCancel(ctx); go a.run() }
func (a *Monitor) domReady(context.Context)    { a.uiOnce.Do(func() { close(a.uiReady) }) }
func (a *Monitor) run() {
	defer close(a.done)
	a.op.Lock()
	root, e := os.UserConfigDir()
	if e == nil {
		a.dir = a.dataDir
		if a.dir == "" {
			a.dir = filepath.Join(root, "SZCapture")
		}
		e = os.MkdirAll(filepath.Join(a.dir, "logs"), 0700)
	}
	if e == nil {
		// Share the old monitor lock so an installed legacy app cannot create a second TUN.
		lockDir := filepath.Join(root, "SZCapture")
		e = os.MkdirAll(lockDir, 0700)
		if e != nil {
			a.set("error", e.Error())
			a.op.Unlock()
			return
		}
		a.lock = flock.New(filepath.Join(lockDir, "app.lock"))
	}
	if e != nil {
		a.set("error", e.Error())
		a.op.Unlock()
		return
	}
	logs := capture.NewLogbook(a.dir)
	if a.logSink != nil {
		if err := logs.Connect(a.logSink); err != nil {
			logs.AddStatus("warn", "历史日志导入失败："+err.Error())
		}
	}
	logs.SetRuntimeEnabled(false)
	a.mu.Lock()
	a.logs = logs
	a.mu.Unlock()

	client := capture.LocalClient()
	defer client.CloseIdleConnections()
	a.mu.Lock()
	a.client = client
	a.mu.Unlock()
	a.op.Unlock()
	if !waitForColdStart(a.ctx, a.uiReady, 1200*time.Millisecond) {
		return
	}
	a.set("starting", "正在检查后端连接与 CA 证书")
	timer := time.NewTicker(3 * time.Second)
	defer timer.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		a.op.Lock()
		a.sync()
		a.op.Unlock()
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		case <-a.wake:
		}
	}
}
func (a *Monitor) stopLease() error {
	if a.lease == nil {
		return nil
	}
	e := a.lease.Stop()
	if e == nil {
		a.lease = nil
	}
	return e
}

// stopLeaseOrBlock stops the lease and, when the stop cannot be confirmed,
// blocks further reconnects until the user retries. It reports whether it blocked.
func (a *Monitor) stopLeaseOrBlock() bool {
	stopErr := a.stopLease()
	if stopErr == nil {
		return false
	}
	a.blocked = true
	a.set("error", "无法确认流量接管已停止："+stopErr.Error())
	return true
}
func (a *Monitor) sync() {
	if a.closed || a.ctx.Err() != nil {
		return
	}
	// A failed stop leaves the previous helper's TUN state unconfirmed. Do not
	// create another lease until the user explicitly retries the connection.
	if a.blocked {
		return
	}
	if a.lock != nil && !a.lock.Locked() {
		ok, err := a.lock.TryLock()
		if err != nil || !ok {
			a.set("error", "旧监视器正在运行或采集锁不可用，请退出旧监视器后重新连接")
			return
		}
	}
	a.mu.Lock()
	a.state.ReceiverURL = a.config.ReceiverURL
	a.state.TokenConfigured = a.config.Token != ""
	before := a.state.Stage
	a.mu.Unlock()

	d, e := capture.Fetch(a.ctx, a.client, a.config)
	if e != nil {
		if a.ctx.Err() != nil {
			return
		}
		if before != "offline" && a.runtimeReady {
			a.logs.Add("warn", "接收服务未连接", e.Error())
		}
		a.pauseRuntime()
		a.mu.Lock()
		a.state.CaptureEnabled = false
		a.state.ProxyAddress = ""
		a.mu.Unlock()
		if a.stopLeaseOrBlock() {
			return
		}
		a.set("offline", e.Error())
		return
	}
	if a.instance != d.ProxyPassword {
		a.pauseRuntime()
		if e = a.stopLease(); e != nil {
			a.blocked = true
			a.set("error", e.Error())
			return
		}
		a.instance = d.ProxyPassword
		a.cursor = 0
	}
	a.mu.Lock()
	a.state.ProxyAddress = d.ProxyAddress
	a.state.CaptureEnabled = d.CaptureEnabled
	a.state.Received = d.Received
	a.mu.Unlock()
	cert, certErr := a.checkCertificate(a.ctx, d)
	if certErr != nil || !cert.Trusted {
		a.pauseRuntime()
		a.consumeBackendLogs(d.Logs, false)
		if a.stopLeaseOrBlock() {
			return
		}
		if certErr != nil {
			a.set("certificate", "证书状态尚未确认，请打开“证书管理”检查")
		} else if !cert.Installed {
			a.set("certificate", "请打开“证书管理”，将证书安装到系统")
		} else {
			a.set("trust", "请打开“证书管理”，信任证书")
		}
		return
	}
	if !a.runtimeReady {
		// Establish a cursor after trust is ready; never replay earlier traffic.
		a.consumeBackendLogs(d.Logs, false)
		a.runtimeReady = true
		a.logs.SetRuntimeEnabled(true)
		a.logs.Add("info", "流量采集初始化完成", "CA 证书已被系统信任")
		a.logs.Add("info", "已连接内置接收服务", d.ProxyAddress)
	} else {
		a.consumeBackendLogs(d.Logs, true)
	}
	if mode, ok := a.lease.(interface {
		NeedsModeSwitch(context.Context) (bool, error)
	}); ok {
		changed, err := mode.NeedsModeSwitch(a.ctx)
		if err != nil {
			a.set("error", err.Error())
			return
		}
		if changed {
			if a.stopLeaseOrBlock() {
				return
			}
			a.logs.Add("info", "代理接管状态已变化，正在自动切换抓包方式", "")
		}
	}
	if a.lease == nil {
		a.set("starting", "正在检查证书与流量接管权限")
		a.lease, e = a.startTunnel(a.ctx, d, a.logs)
		if errors.Is(e, tunnel.ErrCertificateRequired) {
			a.pauseRuntime()
			a.set("trust", "请打开“证书管理”，信任证书")
			return
		}
		if e != nil {
			a.blocked = true
			a.logs.Add("error", "流量接管未启动", e.Error())
			a.set("error", e.Error())
			return
		}
	}
	stage, message := a.lease.State()
	switch stage {
	case "ready":
		if d.CaptureEnabled {
			a.set("ready", "正在采集 SzPlayer 流量，数据直接送入密钥提取流程")
		} else {
			a.set("ready", "SzPlayer 流量接管已就绪 · 等待开始提取")
		}
	case "error", "stopped":
		a.blocked = true
		a.set("error", message)
	default:
		a.set("starting", "正在准备证书和流量接管")
	}
}
func (a *Monitor) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *Monitor) setCaptureRouting(ctx context.Context, enabled bool) error {
	a.op.Lock()
	defer a.op.Unlock()
	if enabled {
		if a.closed || a.ctx == nil || a.client == nil || a.logs == nil {
			return fmt.Errorf("流量接管尚未就绪，请稍后重试")
		}
		a.sync()
		if err := ctx.Err(); err != nil {
			return err
		}
		if state := a.GetState(); state.Stage == "error" || state.Stage == "certificate" || state.Stage == "trust" {
			return fmt.Errorf("%s", state.Message)
		}
	}
	if a.lease == nil {
		if !enabled {
			return nil
		}
		return fmt.Errorf("流量接管尚未就绪，请稍后重试")
	}
	if routing, ok := a.lease.(interface {
		SetCaptureEnabled(context.Context, bool) error
	}); ok {
		return routing.SetCaptureEnabled(ctx, enabled)
	}
	return nil
}
func (a *Monitor) GetState() State {
	a.mu.RLock()
	s := a.state
	logs := a.logs
	a.mu.RUnlock()
	if logs != nil {
		s.Logs = logs.Entries()
	}
	return s
}
func (a *Monitor) Retry() {
	go func() {
		a.op.Lock()
		defer a.op.Unlock()
		if a.closed || a.logs == nil {
			return
		}
		if e := a.stopLease(); e != nil {
			a.blocked = true
			a.set("error", e.Error())
			return
		}
		a.blocked = false
		a.set("starting", "正在重新连接")
		a.signal()
	}()
}
func (a *Monitor) ClearLogs() error {
	a.op.Lock()
	defer a.op.Unlock()
	if a.closed {
		return fmt.Errorf("程序正在退出，无法清空日志")
	}
	if a.logs == nil {
		return nil
	}
	return a.logs.Clear()
}

func (a *Monitor) beforeClose(context.Context) bool {
	a.cancel()
	a.set("starting", "正在退出并释放流量接管")
	a.op.Lock()
	defer a.op.Unlock()
	a.closed = true
	if e := a.stopLease(); e != nil && a.logs != nil {
		a.logs.Add("warn", "流量接管正在自动退出", e.Error())
	}
	return false
}
func (a *Monitor) shutdown(context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	<-a.done
	a.op.Lock()
	defer a.op.Unlock()
	if a.lease != nil {
		if lease, ok := a.lease.(interface{ Abort() error }); ok {
			_ = lease.Abort()
			a.lease = nil
		} else {
			_ = a.stopLease()
		}
	}
	if a.logs != nil {
		a.logs.Close()
	}
	if a.lock != nil {
		a.lock.Unlock()
	}
}
