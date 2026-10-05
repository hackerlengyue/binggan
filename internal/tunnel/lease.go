package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/surge"
	"time.haomen/binggan/v2/internal/system"
)

type Lease struct {
	URL, Token      string
	server          *http.Server
	mu              sync.Mutex
	stopping        bool
	status          string
	detail          string
	stopped         chan struct{}
	once            sync.Once
	logs            *capture.Logbook
	seen            time.Time
	claimed         bool
	surgeMode       bool
	routingEnabled  bool
	routingRevision uint64
	routingApplied  uint64
}
type Report struct {
	Status          string  `json:"status"`
	Message         string  `json:"message"`
	Level           string  `json:"level"`
	RoutingRevision *uint64 `json:"routingRevision,omitempty"`
}

func Start(ctx context.Context, d capture.Descriptor, logs *capture.Logbook) (*Lease, error) {
	active, err := surge.Active(ctx)
	if err != nil {
		return nil, err
	}
	return startInMode(ctx, d, logs, active, prepareDesktopCertificate, system.LaunchHelper, launchSurgeHelper)
}

func startInMode(ctx context.Context, d capture.Descriptor, logs *capture.Logbook, active bool,
	prepare func(context.Context, capture.Descriptor, *capture.Logbook) error,
	tunLaunch, surgeLaunch func(context.Context, string, string, string) error) (*Lease, error) {
	launch := tunLaunch
	if active {
		launch = surgeLaunch
	}
	l, err := start(ctx, d, logs, prepare, launch)
	if err == nil {
		l.mu.Lock()
		l.surgeMode = active
		l.mu.Unlock()
		if active {
			logs.Add("info", "已自动选择 Surge 转发模式", "仅深造域名交由抓包处理，其余流量保留 Surge 原有规则")
		}
	}
	return l, err
}

func start(ctx context.Context, d capture.Descriptor, logs *capture.Logbook,
	prepare func(context.Context, capture.Descriptor, *capture.Logbook) error,
	launch func(context.Context, string, string, string) error) (*Lease, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	if e := prepare(ctx, d, logs); e != nil {
		return nil, e
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	l, e := openLease(d, logs)
	if e != nil {
		return nil, e
	}
	exe, e := os.Executable()
	if e == nil {
		logs.Add("info", "正在连接流量接管服务", "")
		e = launch(ctx, exe, l.URL, l.Token)
	}
	if e != nil {
		l.server.Close()
		return nil, e
	}
	return l, nil
}

// openLease binds the authenticated control channel without changing system state.
func openLease(d capture.Descriptor, logs *capture.Logbook) (*Lease, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}

	d.Logs = nil
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		listener.Close()
		return nil, e
	}
	l := &Lease{URL: "http://" + listener.Addr().String(), Token: hex.EncodeToString(token), status: "starting", seen: time.Now(), logs: logs, stopped: make(chan struct{}), routingEnabled: d.CaptureEnabled}
	l.server = &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+l.Token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/report" {
			var report Report
			if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&report) != nil {
				http.Error(w, "Bad request", 400)
				return
			}
			if report.Status != "" {
				l.mu.Lock()
				if l.status != "error" {
					l.status = report.Status
					l.detail = report.Message
				}
				if report.Status == "ready" && report.RoutingRevision != nil {
					l.routingApplied = *report.RoutingRevision
				}
				l.mu.Unlock()
			}
			if report.Status == "stopped" {
				l.once.Do(func() { close(l.stopped) })
			}
			if report.Message != "" {
				level := report.Level
				if level != "error" && level != "warn" {
					level = "info"
				}
				l.logs.Add(level, report.Message, "")
			}
			w.WriteHeader(204)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/lease" {
			http.NotFound(w, r)
			return
		}
		l.mu.Lock()
		stopping := l.stopping
		current := d
		current.CaptureEnabled = l.routingEnabled
		current.RoutingRevision = l.routingRevision
		l.seen = time.Now()
		if !stopping {
			l.claimed = true
		}
		l.mu.Unlock()
		if stopping {
			w.WriteHeader(410)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(current)
	})}
	go l.server.Serve(listener)
	return l, nil
}

// SetCaptureEnabled waits for the Surge helper to confirm that the exact rule
// was installed/removed before the UI can start playback or finish its capture.
// The original TUN route does not need a per-capture reconfiguration, but
// must also confirm readiness before playback can start.
func (l *Lease) SetCaptureEnabled(ctx context.Context, enabled bool) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	surgeMode := l.surgeMode
	if !surgeMode && !enabled {
		l.mu.Unlock()
		return nil
	}
	if surgeMode && l.routingEnabled != enabled {
		l.routingEnabled = enabled
		l.routingRevision++
	}
	revision := l.routingRevision
	l.mu.Unlock()
	// Failed starts must not leave a late acknowledgement arming forwarding.
	defer func() {
		if result != nil && enabled && surgeMode {
			l.mu.Lock()
			if l.routingRevision == revision {
				l.routingEnabled = false
				l.routingRevision++
			}
			l.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待流量接管确认超时或已取消：%w", err)
		}
		l.mu.Lock()
		status, detail, applied, seen, stopping := l.status, l.detail, l.routingApplied, l.seen, l.stopping
		l.mu.Unlock()
		if stopping {
			return fmt.Errorf("流量接管正在退出，不能开始提取")
		}
		if time.Since(seen) > 20*time.Second {
			return fmt.Errorf("流量接管进程未响应，请重新连接")
		}
		if status == "error" || status == "stopped" {
			return fmt.Errorf("流量接管未就绪：%s", detail)
		}
		if status == "ready" && (!surgeMode || applied == revision) {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

func (l *Lease) NeedsModeSwitch(ctx context.Context) (bool, error) {
	active, err := surge.Active(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	return active != l.surgeMode, err
}
func (l *Lease) State() (string, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.status != "error" && l.status != "stopped" && time.Since(l.seen) > 20*time.Second {
		return "error", "流量接管进程未响应，请重新连接"
	}
	return l.status, l.detail
}
func (l *Lease) Stop() error {
	l.mu.Lock()
	l.stopping = true
	claimed := l.claimed
	l.mu.Unlock()
	if !claimed {
		// The helper has not fetched the descriptor and therefore cannot have
		// started TUN. Reject any late fetch without waiting for a report.
		return l.server.Close()
	}
	select {
	case <-l.stopped:
		l.server.Close()
		return nil
	case <-time.After(12 * time.Second):
		// Keep the control channel alive so a late stopped report can still be
		// observed on Retry. /lease already returns 410 while stopping.
		return fmt.Errorf("接管进程尚未确认退出，请稍候重试；窗口关闭后心跳中断将自动释放 TUN")
	}
}

// Abort is used only during application shutdown after Stop timed out.
// Closing the control channel makes a remaining helper release its TUN after
// the heartbeat fails, even if it never sent a stopped report.
func (l *Lease) Abort() error { return l.server.Close() }
func helperRequest(client *http.Client, base, token, method, path string, value any) (*http.Response, error) {
	var body io.Reader
	if value != nil {
		b, e := json.Marshal(value)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, base+path, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}
func report(client *http.Client, base, token string, r Report) {
	resp, e := helperRequest(client, base, token, "POST", "/report", r)
	if e == nil {
		resp.Body.Close()
	}
}
func readLease(client *http.Client, base, token string) (capture.Descriptor, error) {
	var d capture.Descriptor
	resp, e := helperRequest(client, base, token, "GET", "/lease", nil)
	if e != nil {
		return d, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return d, fmt.Errorf("桌面窗口已关闭")
	}
	e = json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&d)
	if e == nil {
		e = d.Validate()
	}
	return d, e
}
