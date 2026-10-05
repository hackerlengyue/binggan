package tunnel

import (
	"fmt"
	box "github.com/sagernet/sing-box"
	boxlog "github.com/sagernet/sing-box/log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time.haomen/binggan/v2/internal/capture"
	"time"
)

type logWriter struct {
	queue       chan Report
	client      *http.Client
	base, token string
	done        chan struct{}
}

// ansiPattern strips terminal color escapes; sing-box console messages carry
// them and they render as garbage boxes in the log window.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (w *logWriter) WriteMessage(level boxlog.Level, message string) {
	// PlatformLogWriter receives every level regardless of the console filter.
	if level > boxlog.LevelWarn {
		return
	}
	message = ansiPattern.ReplaceAllString(message, "")
	// Per-connection failures of the direct outbound are environmental noise from
	// unrelated apps; only failures on the capture outbound concern this window.
	if strings.Contains(message, "connection:") && strings.Contains(message, "outbound/direct") {
		return
	}
	v := "warn"
	if level <= boxlog.LevelError {
		v = "error"
	}
	if len(message) > 3000 {
		message = message[:3000]
	}
	select {
	case w.queue <- Report{Message: message, Level: v}:
	default:
	}
}
func RunHelper(base, token string) error {
	if !capture.IsLocalURL(base) || len(token) != 64 {
		return fmt.Errorf("无效的本机控制连接")
	}
	client := capture.LocalClient()
	defer client.CloseIdleConnections()
	d, e := readLease(client, base, token)
	if e != nil {
		return e
	}
	defer report(client, base, token, Report{Status: "stopped", Message: "流量接管已退出，TUN 路由已释放"})
	fail := func(e error) error {
		report(client, base, token, Report{Status: "error", Level: "error", Message: e.Error()})
		return e
	}
	if _, e = readLease(client, base, token); e != nil {
		return e
	}
	runtimeDir, e := os.MkdirTemp("", "sz-capture-runtime-")
	if e != nil {
		return fail(fmt.Errorf("创建流量接管临时目录失败：%w", e))
	}
	defer os.RemoveAll(runtimeDir)
	ctx, opts, e := helperOptions(d, runtimeDir)
	if e != nil {
		return fail(e)
	}
	writer := &logWriter{client: client, base: base, token: token, queue: make(chan Report, 128), done: make(chan struct{})}
	defer close(writer.done)
	go func() {
		for {
			select {
			case v := <-writer.queue:
				report(client, base, token, v)
			case <-writer.done:
				return
			}
		}
	}()
	instance, e := box.New(box.Options{Context: ctx, Options: opts, PlatformLogWriter: writer})
	if e != nil {
		return fail(e)
	}
	if e = instance.Start(); e != nil {
		instance.Close()
		return fail(e)
	}
	defer instance.Close()
	report(client, base, token, Report{Status: "ready", Message: "流量接管已就绪，仅 SzPlayer 的流量交由后端处理"})
	failures := 0
	for {
		time.Sleep(time.Second)
		_, e = readLease(client, base, token)
		if e == nil {
			failures = 0
			continue
		}
		if strings.Contains(e.Error(), "窗口已关闭") {
			return nil
		}
		failures++
		if failures >= 3 {
			return nil
		}
	}
}
