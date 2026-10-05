package main

import (
	"context"
	"time.haomen/binggan/v2/internal/capture"
	"time"
)

// Wait for the first rendered window before beginning network/trust checks.
// The short settling interval avoids an immediate authorization prompt or log burst.
func waitForColdStart(ctx context.Context, uiReady <-chan struct{}, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-uiReady:
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Called under a.op; certificate installation events bypass the runtime gate.
func (a *Monitor) pauseRuntime() {
	a.runtimeReady = false
	a.logs.SetRuntimeEnabled(false)
}

func (a *Monitor) consumeBackendLogs(entries []capture.Entry, emit bool) {
	for _, entry := range entries {
		if entry.ID <= a.cursor {
			continue
		}
		a.cursor = entry.ID
		if emit {
			a.logs.AddBackend(entry.Level, entry.Message, entry.Detail)
		}
	}
}
