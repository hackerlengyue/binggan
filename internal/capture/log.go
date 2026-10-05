package capture

import (
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Scope   string `json:"scope,omitempty"`
}
type Logbook struct {
	mu             sync.Mutex
	items          []Entry
	snapshot       []Entry
	next           int64
	closed         bool
	runtimeEnabled bool
	logger         *slog.Logger
	writer         *lumberjack.Logger
	sink           func([]Entry) error
}

func NewLogbook(dir string) *Logbook {
	w := &lumberjack.Logger{Filename: filepath.Join(dir, "logs", "capture.log"), MaxSize: 5, MaxBackups: 3, MaxAge: 14, LocalTime: true}
	return &Logbook{logger: slog.New(slog.NewJSONHandler(w, nil)), writer: w, runtimeEnabled: true, items: []Entry{}}
}
func (l *Logbook) Add(level, message, detail string) {
	l.add(level, message, detail, "runtime")
}

// Backend records are persisted by the owned service already. Preserve the
// diagnostic buffer without inserting them a second time in the unified list.
func (l *Logbook) AddBackend(level, message, detail string) {
	l.add(level, message, detail, "backend")
}

// Certificate actions remain visible while normal runtime logging is gated.
func (l *Logbook) AddCertificate(level, message, detail string) {
	l.add(level, message, detail, "certificate")
}

// Status messages explain setup and connection state even before capture is ready.
func (l *Logbook) AddStatus(level, message string) {
	l.add(level, message, "", "status")
}

func (l *Logbook) SetRuntimeEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runtimeEnabled = enabled
}

func (l *Logbook) add(level, message, detail, scope string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || ((scope == "runtime" || scope == "backend") && !l.runtimeEnabled) {
		return
	}
	l.next++
	v := Entry{ID: l.next, Time: time.Now().Format(time.RFC3339Nano), Level: level, Message: message, Detail: detail, Scope: scope}
	l.items = append(l.items, v)
	if len(l.items) > 500 {
		l.items = append([]Entry(nil), l.items[len(l.items)-500:]...)
	}
	l.snapshot = nil
	if l.sink != nil && scope != "backend" {
		if err := l.sink([]Entry{v}); err != nil {
			l.logger.Error("日志持久化失败", "detail", err.Error(), "scope", "status")
		}
	}
	switch level {
	case "error":
		l.logger.Error(message, "detail", detail, "scope", scope)
	case "warn":
		l.logger.Warn(message, "detail", detail, "scope", scope)
	default:
		l.logger.Info(message, "detail", detail, "scope", scope)
	}
}

// Entries reuses one immutable copy until the next Add or Clear.
// Callers must not modify the returned slice.
func (l *Logbook) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.snapshot == nil {
		l.snapshot = make([]Entry, len(l.items))
		copy(l.snapshot, l.items)
	}
	return l.snapshot
}

// Clear serializes with Add so a live helper can continue logging afterwards.
// Keep the sequence monotonic so an in-flight UI refresh cannot reuse old IDs.
func (l *Logbook) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("日志已关闭")
	}
	if err := l.writer.Close(); err != nil {
		return fmt.Errorf("关闭运行日志失败：%w", err)
	}
	dir := filepath.Dir(l.writer.Filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.Type().IsRegular() || !isLogBackup(file.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, file.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清空历史运行日志失败：%w", err)
		}
	}
	f, err := os.OpenFile(l.writer.Filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("清空运行日志失败：%w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	l.items = []Entry{}
	l.snapshot = nil
	return nil
}

func isLogBackup(name string) bool {
	name = strings.TrimSuffix(name, ".gz")
	if !strings.HasPrefix(name, "capture-") || !strings.HasSuffix(name, ".log") {
		return false
	}
	_, err := time.Parse("2006-01-02T15-04-05.000", strings.TrimSuffix(strings.TrimPrefix(name, "capture-"), ".log"))
	return err == nil
}

func (l *Logbook) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return l.writer.Close()
}
