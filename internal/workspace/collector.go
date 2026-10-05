package workspace

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"time.haomen/binggan/v2/internal/captureproxy"

	"time.haomen/binggan/v2/internal/store"
)

type collector struct {
	app      *App
	ca       captureproxy.CA
	proxy    *captureproxy.Proxy
	relay    *captureproxy.Relay
	mu       sync.Mutex
	logs     []collectorLog
	sequence int64
	received atomic.Int64
	ready    atomic.Bool
	closed   bool
	password string
}
type collectorLog struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

// CollectorDescriptor is returned only to the local capture helper. It
// contains the proxy password and must never be exposed by a page binding.
type CollectorDescriptor struct {
	Version           int            `json:"version"`
	BackendExecutable string         `json:"backendExecutable"`
	Ready             bool           `json:"ready"`
	ProxyAddress      string         `json:"proxyAddress"`
	ProxyUsername     string         `json:"proxyUsername"`
	ProxyPassword     string         `json:"proxyPassword"`
	Certificate       string         `json:"certificate"`
	Fingerprint       string         `json:"fingerprint"`
	CaptureEnabled    bool           `json:"captureEnabled"`
	Received          int64          `json:"received"`
	Logs              []collectorLog `json:"logs"`
}

func (a *App) initCollector() error {
	dir := filepath.Join(a.Config.DataDir, "collector")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	ca, e := captureproxy.LoadCA(dir)
	if e != nil {
		return e
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	a.collector = &collector{app: a, ca: ca, logs: []collectorLog{}, password: hex.EncodeToString(secret)}
	return nil
}
func (a *App) StartCollector() error {
	port := 18767
	if v := os.Getenv("CAPTURE_PROXY_PORT"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 65535 {
			return fmt.Errorf("CAPTURE_PROXY_PORT 须为 1–65535 的整数")
		}
		port = n
	}
	c := a.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startLocked(port)
}
func (c *collector) startLocked(port int) error {
	if c.closed {
		return fmt.Errorf("采集服务已关闭")
	}
	if c.proxy != nil {
		return nil
	}
	p := captureproxy.NewProxy("127.0.0.1:0", c.ca, c)
	handler := p.Server.Handler
	p.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The internal decoder uses separate authentication from the UI API.
		authorization := r.Header.Get("Proxy-Authorization")
		raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(authorization, "Basic "))
		user, password, ok := strings.Cut(string(raw), ":")
		if !ok || user != "sz-capture" || subtle.ConstantTimeCompare([]byte(password), []byte(c.password)) != 1 {
			w.Header().Set("Proxy-Authenticate", `Basic realm="饼干大小姐"`)
			http.Error(w, "Proxy authentication required", 407)
			return
		}
		r.Header.Del("Proxy-Authorization")
		handler.ServeHTTP(w, r)
	})
	if e := p.Start(); e != nil {
		return e
	}
	relay, e := captureproxy.NewRelay(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), p.Server.Addr, c.password)
	if e != nil {
		p.Close()
		return e
	}
	c.proxy = p
	c.relay = relay
	c.ready.Store(true)

	return nil
}
func (c *collector) rotateCA() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("采集服务已关闭")
	}
	wasRunning := c.proxy != nil
	port := 18767
	if c.relay != nil {
		if _, p, e := net.SplitHostPort(c.relay.Address); e == nil {
			if n, e := strconv.Atoi(p); e == nil {
				port = n
			}
		}
		c.relay.Close()
		c.relay = nil
	}
	if c.proxy != nil {
		c.proxy.Close()
		c.proxy = nil
	}
	c.ready.Store(false)
	ca, e := captureproxy.ReplaceCA(filepath.Dir(c.ca.Path))
	if e != nil {
		return e
	}
	c.ca = ca
	if !wasRunning {
		return nil
	}
	if e = c.startLocked(port); e != nil {
		return fmt.Errorf("新证书已生成，但抓包入口未能重新启动：%w", e)
	}
	return nil
}
func (a *App) RegenerateCollectorCertificate() (CollectorDescriptor, error) {
	if err := a.collector.rotateCA(); err != nil {
		return CollectorDescriptor{}, err
	}
	return a.CollectorDescriptor()
}
func (c *collector) Enabled() bool {
	c.app.captureMu.Lock()
	defer c.app.captureMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.app.capture && !c.closed
}
func (c *collector) Accept(pair captureproxy.Pair) {
	a := c.app
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !a.capture || closed {
		return
	}
	tx, e := a.Store.DB.Begin()
	if e != nil {
		c.Log("error", "抓包数据保存失败", e.Error())
		return
	}
	defer tx.Rollback()
	events := make([]Capture, 0, 2)
	pairID := store.ID()
	for _, v := range []captureproxy.Event{pair.Request, pair.Response} {
		headers := map[string]any{}
		for k, value := range v.Headers {
			headers[k] = value
		}
		event := Capture{ID: store.ID(), RequestID: pairID, Phase: v.Phase, TS: v.TS, Method: v.Method, URL: v.URL, Host: v.Host, Headers: headers, Body: v.Body, BodyTruncated: v.BodyTruncated, Source: "sing-box"}
		if v.Phase == "response" {
			status := v.Status
			event.Status = &status
		}
		if _, e = tx.Exec("INSERT INTO captures(payload) VALUES(?)", store.Encode(event)); e != nil {
			tx.Rollback()
			c.Log("error", "抓包数据保存失败", e.Error())
			return
		}
		events = append(events, event)
	}
	if e = tx.Commit(); e != nil {
		c.Log("error", "抓包数据保存失败", e.Error())
		return
	}
	for _, v := range events {
		item := v
		a.hub.broadcast(CaptureNotice{Kind: "message", Capture: &item})
	}
	c.received.Add(1)
	c.Log("info", "请求与响应已接收", fmt.Sprintf("%s %s · HTTP %d", pair.Request.Method, pair.Path, pair.Response.Status))
}
func (c *collector) Log(level, message, detail string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.sequence++
	c.logs = append(c.logs, collectorLog{c.sequence, time.Now().Format(time.RFC3339Nano), level, message, detail})
	if len(c.logs) > 200 {
		c.logs = append([]collectorLog(nil), c.logs[len(c.logs)-200:]...)
	}
	c.app.log(level, "capture", message, detail, "")
	c.mu.Unlock()
}
func (a *App) CollectorDescriptor() (CollectorDescriptor, error) {
	c := a.collector
	enabled := c.Enabled()
	c.mu.Lock()
	logs := append([]collectorLog{}, c.logs...)
	address := ""
	if c.relay != nil {
		address = c.relay.Address
	}
	password := c.password
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.ca.Certificate.Certificate[0]}))
	fingerprint := c.ca.Fingerprint
	c.mu.Unlock()
	exe, err := os.Executable()
	if err != nil {
		return CollectorDescriptor{}, err
	}
	return CollectorDescriptor{2, exe, c.ready.Load(), address, "sz-capture", password, certificate, fingerprint, enabled, c.received.Load(), logs}, nil
}
func (c *collector) Close() {
	c.app.captureMu.Lock()
	c.mu.Lock()
	c.closed = true
	p := c.proxy
	relay := c.relay
	c.mu.Unlock()
	c.app.captureMu.Unlock()
	c.ready.Store(false)
	if relay != nil {
		relay.Close()
	}
	if p != nil {
		p.Close()
	}
}
