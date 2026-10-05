//go:build darwin

package main

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/playerautomation"
)

type permissionStatusFixture struct {
	playerautomation.Driver
	granted atomic.Bool
	checked chan struct{}
}

func (f *permissionStatusFixture) Status() playerautomation.Status {
	select {
	case f.checked <- struct{}{}:
	default:
	}
	return playerautomation.Status{AccessibilityReady: f.granted.Load()}
}

func TestPermissionFlowClosesWhenAppBecomesTrusted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture := &permissionStatusFixture{checked: make(chan struct{}, 1)}
	cmd := exec.Command("/bin/sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("permission helper process did not exit")
		}
	})
	app := &Workbench{ctx: ctx, player: fixture, permissionCmd: cmd}
	go watchPermissionFlowGrant(app, cmd, done)
	select {
	case <-fixture.checked:
	case <-time.After(2 * time.Second):
		t.Fatal("permission status was not checked")
	}
	select {
	case <-done:
		t.Fatal("permission helper exited before access was granted")
	default:
	}
	fixture.granted.Store(true)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("permission helper remained after access was granted")
	}
	app.permissionMu.Lock()
	current := app.permissionCmd
	app.permissionMu.Unlock()
	if current != nil {
		t.Fatal("closed permission helper remained active")
	}
}
