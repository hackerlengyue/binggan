package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The channel's context owns this hold. Closing/reloading the renderer releases
// it even if the JavaScript queue cannot reach its finally block.
func (a *Monitor) holdCaptureRouting(ctx context.Context, ready func(string) error, stopReceiver func()) (result error) {
	a.op.Lock()
	if err := ctx.Err(); err != nil {
		a.op.Unlock()
		return err
	}
	if a.closed || a.ctx == nil {
		a.op.Unlock()
		return fmt.Errorf("流量接管尚未就绪，请稍后重试")
	}
	if a.routingHeld != "" || a.captureRequested {
		a.op.Unlock()
		return fmt.Errorf("已有其他采集任务持有分流连接")
	}
	a.routingHeld = uuid.NewString()
	owner := a.routingHeld
	a.op.Unlock()
	defer func() {
		a.op.Lock()
		defer a.op.Unlock()
		a.routingHeld = ""
		a.captureRequested = false
		stopReceiver()
		if !a.routingRetained {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 9*time.Second)
		defer cancel()
		if err := a.applyCaptureRouting(cleanup, false); err != nil {
			a.blocked = true
			a.set("error", "队列结束后恢复分流失败："+err.Error())
			result = errors.Join(result, err)
		}
	}()
	if err := ready(owner); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-a.ctx.Done():
	}
	return nil
}

func (a *Monitor) setCaptureRouting(ctx context.Context, enabled bool, owner string, setReceiver func(bool)) error {
	a.op.Lock()
	defer a.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner != a.routingHeld {
		return fmt.Errorf("采集任务与队列分流连接不匹配")
	}
	if !enabled {
		setReceiver(false)
		a.captureRequested = false
		if a.routingHeld != "" {
			return nil
		}
	} else if a.routingHeld != "" && a.routingRetained {
		if a.closed || a.lease == nil {
			return fmt.Errorf("队列分流连接已断开")
		}
		stage, message := a.lease.State()
		if stage != "ready" {
			return fmt.Errorf("队列分流连接未就绪：%s", message)
		}
		a.captureRequested = true
		setReceiver(true)
		return nil
	}
	if err := a.applyCaptureRouting(ctx, enabled); err != nil {
		return err
	}
	a.captureRequested = enabled
	setReceiver(enabled)
	return nil
}

// Caller holds op. Per-course receiver toggles never call this while a queue
// retains an already-confirmed route; manual captures keep the original flow.
func (a *Monitor) applyCaptureRouting(ctx context.Context, enabled bool) error {
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
			a.routingRetained = false
			return nil
		}
		return fmt.Errorf("流量接管尚未就绪，请稍后重试")
	}
	if routing, ok := a.lease.(interface {
		SetCaptureEnabled(context.Context, bool) error
	}); ok {
		if err := routing.SetCaptureEnabled(ctx, enabled); err != nil {
			return err
		}
	}
	a.routingRetained = enabled
	return nil
}
