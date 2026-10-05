// Package workspace owns the application data and business operations.
// Desktop bindings and the restricted collector helper are its callers.
package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/tasks"
)

type Connection struct {
	Provider string `json:"provider"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type App struct {
	Config     config.Config
	Store      *store.Store
	Tasks      *tasks.Manager
	captureMu  sync.Mutex
	capture    bool
	hub        *hub
	uploads    chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	logClearMu sync.RWMutex
	logClearer func() error
	collector  *collector
}

func New(cfg config.Config, data *store.Store) (*App, error) {
	manager, err := tasks.New(data, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{Config: cfg, Store: data, Tasks: manager, hub: newHub(), uploads: make(chan struct{}, 2), ctx: ctx, cancel: cancel}
	if err := app.initCollector(); err != nil {
		cancel()
		manager.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) Close() {
	if a.collector != nil {
		a.collector.Close()
	}
	a.cancel()
	a.hub.close()
	a.Tasks.Close()
}

func (a *App) log(level, source, message, detail, id string) {
	if err := a.Store.Log(level, source, message, detail, id); err != nil {
		slog.Error("日志持久化失败", "error", err)
	}
	if level == "error" {
		slog.Error(message, "source", source, "request_id", id, "detail", detail)
	}
}

// AuditOperation records binding calls without storing their arguments or raw
// errors, which may contain video paths, URLs, or captured secrets.
func (a *App) AuditOperation(action string, elapsed time.Duration, recordSuccess bool, callErr error) {
	if callErr == nil && !recordSuccess {
		return
	}
	level, outcome := "info", "成功"
	if callErr != nil {
		level, outcome = "warn", "失败"
	}
	detail := fmt.Sprintf("通道：MyGo 绑定\n耗时：%.1f 毫秒", float64(elapsed.Microseconds())/1000)
	a.log(level, "request", action+outcome, detail, store.ID())
}
