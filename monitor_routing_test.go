package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/config"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/tunnel"
	"time.haomen/binggan/v2/internal/workspace"
)

type routingTestLease struct {
	calls    []bool
	failStop bool
	stage    string
}

func (l *routingTestLease) State() (string, string) { return l.stage, "fixture" }
func (l *routingTestLease) Stop() error             { return nil }
func (l *routingTestLease) SetCaptureEnabled(_ context.Context, enabled bool) error {
	l.calls = append(l.calls, enabled)
	if !enabled && l.failStop {
		return errors.New("restore failed")
	}
	return nil
}
func routingFixture(t *testing.T) (*Monitor, *WorkspaceService, *routingTestLease) {
	t.Helper()
	root := t.TempDir()
	if err := workspace.EnsureDirectories(root); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Root: root, DataDir: filepath.Join(root, "data")}
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := workspace.New(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Close(); _ = db.Close() })
	m := newMonitor()
	m.ctx = context.Background()
	m.dir = t.TempDir()
	m.logs = capture.NewLogbook(m.dir)
	t.Cleanup(func() { _ = m.logs.Close() })
	d := testDescriptor(t)
	m.config = capture.DefaultConfig()
	m.client = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		d.CaptureEnabled = app.CaptureState()
		body, _ := json.Marshal(d)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	m.checkCertificate = func(context.Context, capture.Descriptor) (tunnel.CertificateState, error) {
		return tunnel.CertificateState{Trusted: true, Installed: true}, nil
	}
	lease := &routingTestLease{stage: "ready"}
	m.startTunnel = func(context.Context, capture.Descriptor, *capture.Logbook) (captureLease, error) { return lease, nil }
	ready := make(chan struct{})
	close(ready)
	service := &WorkspaceService{&Workbench{ready: ready, app: app, monitor: m}}
	return m, service, lease
}
func startRoutingHold(t *testing.T, m *Monitor, service *WorkspaceService) (string, func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- m.holdCaptureRouting(ctx, func(owner string) error { ready <- owner; return nil }, func() { service.workbench.app.SetCaptureState(false) })
	}()
	var owner string
	select {
	case owner = <-ready:
	case err := <-done:
		t.Fatalf("hold failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("hold readiness timed out")
	}
	release := func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(time.Second):
			return errors.New("hold cleanup timed out")
		}
	}
	t.Cleanup(cancel)
	return owner, release
}
func TestQueueKeepsRoutingAcrossSeparateCaptureRecords(t *testing.T) {
	m, s, lease := routingFixture(t)
	owner, release := startRoutingHold(t, m, s)
	for course := 0; course < 3; course++ {
		if err := s.ClearTraces(context.Background(), "queue", owner); err != nil {
			t.Fatal(err)
		}
		enabled, err := s.SetCaptureState(context.Background(), true, owner)
		if err != nil || !enabled {
			t.Fatalf("start: %v %v", enabled, err)
		}
		enabled, err = s.SetCaptureState(context.Background(), false, owner)
		if err != nil || enabled {
			t.Fatalf("stop receiver: %v %v", enabled, err)
		}
		if !reflect.DeepEqual(lease.calls, []bool{true}) {
			t.Fatalf("course %d rebuilt forwarding: %v", course, lease.calls)
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lease.calls, []bool{true, false}) {
		t.Fatalf("queue did not release once: %v", lease.calls)
	}
}
func TestLostQueuePageStopsReceiverAndRestoresRouting(t *testing.T) {
	m, s, lease := routingFixture(t)
	owner, release := startRoutingHold(t, m, s)
	if _, err := s.SetCaptureState(context.Background(), true, owner); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if s.workbench.app.CaptureState() || !reflect.DeepEqual(lease.calls, []bool{true, false}) {
		t.Fatalf("page loss left capture active: %v", lease.calls)
	}
	if _, err := s.SetCaptureState(context.Background(), true, owner); err == nil {
		t.Fatal("late queue callback was accepted")
	}
}
func TestQueueOwnershipProtectsManualCaptureAndBuffer(t *testing.T) {
	m, s, lease := routingFixture(t)
	if _, err := s.SetCaptureState(context.Background(), true, ""); err != nil {
		t.Fatal(err)
	}
	err := m.holdCaptureRouting(context.Background(), func(string) error { t.Fatal("manual capture was taken over"); return nil }, func() {})
	if err == nil {
		t.Fatal("queue took an active manual capture")
	}
	if _, err = s.SetCaptureState(context.Background(), false, ""); err != nil {
		t.Fatal(err)
	}
	owner, release := startRoutingHold(t, m, s)
	if _, err = s.SetCaptureState(context.Background(), true, owner); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if _, err = s.SetCaptureState(context.Background(), enabled, ""); err == nil {
			t.Fatal("manual action changed queue capture")
		}
	}
	if err = s.ClearTraces(context.Background(), "manual", ""); err == nil {
		t.Fatal("manual action cleared queue records")
	}
	if !s.workbench.app.CaptureState() {
		t.Fatal("rejected manual stop disabled queue receiver")
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetCaptureState(context.Background(), true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetCaptureState(context.Background(), false, ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lease.calls, []bool{true, false, true, false, true, false}) {
		t.Fatal(lease.calls)
	}
}
func TestQueueCleanupFailureIsReportedAndBlocksReconnect(t *testing.T) {
	m, s, lease := routingFixture(t)
	owner, release := startRoutingHold(t, m, s)
	if _, err := s.SetCaptureState(context.Background(), true, owner); err != nil {
		t.Fatal(err)
	}
	lease.failStop = true
	if err := release(); err == nil || !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("cleanup failure lost: %v", err)
	}
	if !m.blocked || m.GetState().Stage != "error" || s.workbench.app.CaptureState() {
		t.Fatal("failed cleanup was reported as ready or left receiver on")
	}
}
