package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/tunnel"
)

type testLease struct{ stage string }

func (l *testLease) State() (string, string) { return l.stage, "test state" }
func (l *testLease) Stop() error             { return nil }

type unconfirmedLease struct{ stops int }

func (*unconfirmedLease) State() (string, string) { return "ready", "" }
func (l *unconfirmedLease) Stop() error {
	l.stops++
	return errors.New("helper did not confirm stop")
}

func runtimeLogCount(a *Monitor) int {
	count := 0
	for _, entry := range a.GetState().Logs {
		if entry.Scope == "runtime" || entry.Scope == "backend" {
			count++
		}
	}
	return count
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDescriptor(t *testing.T) capture.Descriptor {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated startup CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	executable, _ := os.Executable()
	return capture.Descriptor{Version: 2, Ready: true, BackendExecutable: executable, ProxyAddress: "127.0.0.1:18767", ProxyUsername: "sz-capture", ProxyPassword: strings.Repeat("b", 64), Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Fingerprint: hex.EncodeToString(sum[:]), Logs: []capture.Entry{{ID: 1, Level: "warn", Message: "previous session"}}}
}

func TestStartupLogsWaitForTrustAndNeverReplayEarlierTraffic(t *testing.T) {
	a := newMonitor()
	lease := &testLease{stage: "starting"}
	starts := 0
	a.startTunnel = func(context.Context, capture.Descriptor, *capture.Logbook) (captureLease, error) {
		starts++
		return lease, nil
	}
	a.ctx = context.Background()
	a.dir = t.TempDir()
	a.logs = capture.NewLogbook(a.dir)
	a.logs.SetRuntimeEnabled(false)
	defer a.logs.Close()
	a.config = capture.DefaultConfig()
	d := testDescriptor(t)
	offline, trusted, installed := false, false, false
	a.client = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		if offline {
			return nil, errors.New("offline")
		}
		body, _ := json.Marshal(d)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	a.checkCertificate = func(context.Context, capture.Descriptor) (tunnel.CertificateState, error) {
		return tunnel.CertificateState{Trusted: trusted, Installed: installed}, nil
	}
	// Stub only the system tunnel; exercise the production startup/status flow.
	for i := 0; i < 3; i++ {
		a.sync()
	}
	if runtimeLogCount(a) != 0 || a.GetState().Stage != "certificate" || starts != 0 {
		t.Fatal("untrusted startup emitted runtime logs")
	}
	installed = true
	a.sync()
	if a.GetState().Stage != "trust" || a.GetState().Message != "请打开“证书管理”，信任证书" || runtimeLogCount(a) != 0 || starts != 0 {
		t.Fatal("installed certificate did not wait for trust")
	}
	offline = true
	a.sync()
	if runtimeLogCount(a) != 0 {
		t.Fatal("offline before certificate trust emitted logs")
	}
	offline = false
	a.logs.AddCertificate("info", "certificate authorization requested", "")
	trusted = true
	a.sync()
	if !a.runtimeReady || starts != 1 {
		t.Fatal("runtime did not start after trust")
	}
	for _, entry := range a.GetState().Logs {
		if entry.Message == "previous session" {
			t.Fatal("startup replayed old traffic")
		}
	}
	if a.GetState().Stage != "starting" {
		t.Fatal("pending tunnel reported running")
	}
	lease.stage = "ready"
	a.sync()
	if a.GetState().Stage != "ready" {
		t.Fatal("ready tunnel did not report running")
	}
	lease.stage = "error"
	a.sync()
	if a.GetState().Stage != "error" {
		t.Fatal("failed tunnel did not report error")
	}
	a.blocked = false
	lease.stage = "ready"
	d.Logs = append(d.Logs, capture.Entry{ID: 2, Level: "info", Message: "fresh traffic"})
	a.sync()
	a.sync()
	count := 0
	for _, entry := range a.GetState().Logs {
		if entry.Message == "fresh traffic" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("fresh traffic appeared %d times", count)
	}
	// Revoking trust also silences late helper reports and skips untrusted traffic.
	trusted = false
	a.sync()
	before := runtimeLogCount(a)
	a.logs.Add("error", "late helper failure", "")
	d.Logs = append(d.Logs, capture.Entry{ID: 3, Level: "warn", Message: "traffic while untrusted"})
	a.sync()
	if runtimeLogCount(a) != before {
		t.Fatal("logs continued after trust was revoked")
	}
	trusted = true
	a.sync()
	for _, entry := range a.GetState().Logs {
		if entry.Message == "traffic while untrusted" {
			t.Fatal("untrusted traffic replayed after trust restored")
		}
	}
	// A restarted backend can reset its log sequence; its history must still be skipped.
	d.ProxyPassword = strings.Repeat("c", 64)
	d.Logs = []capture.Entry{{ID: 1, Level: "warn", Message: "restarted backend history"}}
	a.sync()
	for _, entry := range a.GetState().Logs {
		if entry.Message == "restarted backend history" {
			t.Fatal("backend restart replayed history")
		}
	}
}

func TestColdStartWaitsForWindowAndCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan bool, 1)
	go func() { done <- waitForColdStart(ctx, ready, 15*time.Millisecond) }()
	select {
	case <-done:
		t.Fatal("startup began before window was ready")
	case <-time.After(20 * time.Millisecond):
	}
	start := time.Now()
	close(ready)
	if !<-done || time.Since(start) < 15*time.Millisecond {
		t.Fatal("cold startup settling interval skipped")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if waitForColdStart(canceled, make(chan struct{}), time.Hour) {
		t.Fatal("closed window still started")
	}
}

func TestStatusMessagesAreLoggedOnlyWhenChanged(t *testing.T) {
	a := newMonitor()
	a.logs = capture.NewLogbook(t.TempDir())
	a.logs.SetRuntimeEnabled(false)
	defer a.logs.Close()
	a.set("trust", "请信任证书")
	a.set("trust", "请信任证书")
	a.set("ready", "等待网页开始提取")
	a.set("ready", "等待网页开始提取")
	entries := a.GetState().Logs
	if len(entries) != 2 || entries[0].Scope != "status" || entries[0].Level != "warn" || entries[1].Message != "等待网页开始提取" {
		t.Fatalf("unexpected status entries: %+v", entries)
	}
	if err := a.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	a.set("ready", "等待网页开始提取")
	if len(a.GetState().Logs) != 0 {
		t.Fatal("unchanged status reappeared after clear")
	}
	a.set("ready", "正在采集")
	if len(a.GetState().Logs) != 1 {
		t.Fatal("changed status was not logged")
	}
}

func TestUnconfirmedLeaseStopBlocksAutomaticReconnect(t *testing.T) {
	a := newMonitor()
	a.ctx = context.Background()
	a.dir = t.TempDir()
	a.logs = capture.NewLogbook(a.dir)
	defer a.logs.Close()
	a.config = capture.DefaultConfig()
	lease := &unconfirmedLease{}
	a.lease = lease
	a.client = &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("receiver offline")
	})}
	a.sync()
	if a.lease != lease || !a.blocked || a.GetState().Stage != "error" || lease.stops != 1 {
		t.Fatalf("unconfirmed stop: lease=%v blocked=%v stage=%s stops=%d", a.lease, a.blocked, a.GetState().Stage, lease.stops)
	}
	a.sync()
	if lease.stops != 1 {
		t.Fatalf("automatic retry touched an unconfirmed lease %d times", lease.stops)
	}
}
