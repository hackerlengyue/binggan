package workspace

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/captureproxy"
)

func TestCollectorCannotRestartAfterClose(t *testing.T) {
	app := testApp(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_PROXY_PORT", port)
	app.collector.Close()
	if err := app.StartCollector(); err == nil {
		t.Error("closed collector restarted its listeners")
	}
	if app.collector.ready.Load() {
		t.Error("closed collector reported ready")
	}
}

func TestCollectorPreservesPairAndStopBarrier(t *testing.T) {
	app := testApp(t)
	collector := app.collector
	pair := captureproxy.Pair{Request: captureproxy.Event{Phase: "request", TS: time.Now().UTC().Format(time.RFC3339Nano), Method: "GET", URL: "https://learn.shenzaokeji.com/api/key", Host: "learn.shenzaokeji.com", Headers: map[string]string{"UIT": "fixture"}}, Response: captureproxy.Event{Phase: "response", TS: time.Now().UTC().Format(time.RFC3339Nano), Method: "GET", URL: "https://learn.shenzaokeji.com/api/key", Host: "learn.shenzaokeji.com", Status: 200, Body: "fixture", Headers: map[string]string{}}}
	collector.Accept(pair)
	if collector.received.Load() != 0 {
		t.Fatal("stored while disabled")
	}
	app.SetCaptureState(true)
	collector.Accept(pair)
	events, err := app.History(2500)
	if err != nil || len(events) != 2 {
		t.Fatalf("capture pair = %+v, %v", events, err)
	}
	x, y := events[0], events[1]
	if x.RequestID == "" || x.RequestID != y.RequestID || x.ID == y.ID || x.Source != "sing-box" || y.Source != "sing-box" {
		t.Fatal("pair identifiers invalid")
	}
	app.SetCaptureState(false)
	collector.Accept(pair)
	if collector.received.Load() != 1 {
		t.Fatal("stop barrier failed")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			collector.Log("warn", "fixture", "")
			collector.Accept(pair)
		}
	}()
	collector.Close()
	wg.Wait()
	if collector.Enabled() {
		t.Fatal("closed collector enabled")
	}
}

func TestCollectorPersistenceFailureReleasesTransactionBeforeLogging(t *testing.T) {
	app := testApp(t)
	app.SetCaptureState(true)
	if _, err := app.Store.DB.Exec("CREATE TRIGGER fail_capture BEFORE INSERT ON captures BEGIN SELECT RAISE(ABORT, 'capture fixture failure'); END"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		app.collector.Accept(captureproxy.Pair{Request: captureproxy.Event{Phase: "request"}, Response: captureproxy.Event{Phase: "response"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("failed capture retained the database connection while logging")
	}
	logs := logPage(t, app, LogQuery{Search: "capture fixture failure"})
	if logs.Total != 1 || logs.Items[0].Source != "capture" {
		t.Fatalf("collector failure log = %+v", logs)
	}
}

func TestRegenerateCertificateReplacesFingerprint(t *testing.T) {
	app := testApp(t)
	before := app.collector.ca.Fingerprint
	descriptor, err := app.RegenerateCollectorCertificate()
	if err != nil || descriptor.Fingerprint == "" || descriptor.Fingerprint == before || descriptor.Fingerprint != app.collector.ca.Fingerprint || !strings.Contains(descriptor.Certificate, "BEGIN CERTIFICATE") {
		t.Fatalf("replacement descriptor = %+v, %v", descriptor, err)
	}
}

func TestCaptureConnectionUsesOwnedService(t *testing.T) {
	app := testApp(t)
	status, err := app.Connection()
	if err != nil || status.Active.Provider != "sing-box" || status.Active.Host != app.Config.Host || status.Active.Port != app.Config.Port || status.RestartRequired {
		t.Fatalf("connection = %+v, %v", status, err)
	}
}
