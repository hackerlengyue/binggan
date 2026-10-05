package tunnel

import (
	"context"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/capture"
)

func TestSurgeCaptureWaitsForMatchingRoutingConfirmation(t *testing.T) {
	l, err := openLease(descriptor(t), capture.NewLogbook(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Abort()
	l.surgeMode = true
	client := capture.LocalClient()
	defer client.CloseIdleConnections()
	zero := uint64(0)
	report(client, l.URL, l.Token, Report{Status: "ready", RoutingRevision: &zero})
	done := make(chan error, 1)
	go func() { done <- l.SetCaptureEnabled(context.Background(), true) }()
	var d capture.Descriptor
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		d, err = readLease(client, l.URL, l.Token)
		if err != nil {
			t.Fatal(err)
		}
		if d.CaptureEnabled {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !d.CaptureEnabled || d.RoutingRevision != 1 {
		t.Fatal("helper did not receive desired routing state")
	}
	select {
	case err := <-done:
		t.Fatalf("released before routing confirmed: %v", err)
	default:
	}
	report(client, l.URL, l.Token, Report{Status: "ready", RoutingRevision: &d.RoutingRevision})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("matching confirmation did not release capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err = l.SetCaptureEnabled(ctx, false); err == nil {
		t.Fatal("stop must also wait for rule removal")
	}
}

func TestSurgeStartTimeoutDisarmsLateForwarding(t *testing.T) {
	l := &Lease{surgeMode: true, status: "ready", seen: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := l.SetCaptureEnabled(ctx, true); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatal("expected timeout", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.routingEnabled || l.routingRevision != 2 {
		t.Fatal("late helper could leave forwarding enabled after failed start")
	}
}

func TestOriginalTunModeDoesNotWaitForSurge(t *testing.T) {
	l := &Lease{status: "ready", seen: time.Now()}
	if err := l.SetCaptureEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if l.routingEnabled || l.routingRevision != 0 {
		t.Fatal("original TUN mode was modified")
	}
}

func TestCaptureWaitsForOriginalTunToBeReady(t *testing.T) {
	l := &Lease{status: "starting", seen: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.SetCaptureEnabled(ctx, true) }()
	select {
	case err := <-done:
		t.Fatalf("capture started before TUN readiness: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	l.mu.Lock()
	l.status = "ready"
	l.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("ready TUN did not release capture")
	}
}

func TestCaptureRejectsCancelledOrStaleReadyTransport(t *testing.T) {
	for _, surgeMode := range []bool{false, true} {
		l := &Lease{surgeMode: surgeMode, status: "ready", seen: time.Now()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := l.SetCaptureEnabled(ctx, false); err == nil {
			t.Fatal("cancelled control request was acknowledged")
		}
		l.seen = time.Now().Add(-21 * time.Second)
		ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
		if err := l.SetCaptureEnabled(ctx, true); err == nil {
			t.Fatal("stale transport was accepted as ready")
		}
		cancel()
	}
}

func TestAutomaticModeUsesOnlyTheSelectedTransport(t *testing.T) {
	for _, active := range []bool{false, true} {
		var tunCalls, surgeCalls int
		l, err := startInMode(context.Background(), descriptor(t), capture.NewLogbook(t.TempDir()), active,
			func(context.Context, capture.Descriptor, *capture.Logbook) error { return nil },
			func(context.Context, string, string, string) error { tunCalls++; return nil },
			func(context.Context, string, string, string) error { surgeCalls++; return nil },
		)
		if err != nil {
			t.Fatal(err)
		}
		if active && (surgeCalls != 1 || tunCalls != 0) {
			t.Fatal("Surge mode started a competing TUN")
		}
		if !active && (tunCalls != 1 || surgeCalls != 0) {
			t.Fatal("normal mode did not retain original TUN")
		}
		if l.surgeMode != active {
			t.Fatal("lease transport does not match selected mode")
		}
		l.Stop()
	}
}
