//go:build darwin

package tunnel

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/system"
)

func sendCaptureServiceRequest(t *testing.T, socket string, request system.CaptureServiceRequest) system.CaptureServiceResponse {
	t.Helper()
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response system.CaptureServiceResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func captureTestSocket(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "sz-capture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return filepath.Join(directory, "service.sock")
}

func TestCaptureServiceChecksPeerLeaseAndSingleSession(t *testing.T) {
	logs := capture.NewLogbook(t.TempDir())
	defer logs.Close()
	lease, err := openLease(descriptor(t), logs)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.server.Close()
	request := system.CaptureServiceRequest{Protocol: system.CaptureServiceProtocol, URL: lease.URL, Token: lease.Token}
	socket := captureTestSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	stopRun := make(chan struct{})
	started := make(chan struct{}, 1)
	go func() {
		done <- serveCaptureService(ctx, socket, os.Geteuid(), false, func(url, token string) error {
			if url != lease.URL || token != lease.Token {
				t.Errorf("service passed the wrong lease to helper")
			}
			started <- struct{}{}
			<-stopRun
			return nil
		}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("service did not start: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("service did not start")
	}
	invalidProtocol := request
	invalidProtocol.Protocol++
	if response := sendCaptureServiceRequest(t, socket, invalidProtocol); !response.UpgradeRequired {
		t.Fatalf("protocol mismatch was accepted: %+v", response)
	}
	invalidURL := request
	invalidURL.URL = "http://example.com:1234"
	if response := sendCaptureServiceRequest(t, socket, invalidURL); response.OK {
		t.Fatal("remote lease was accepted")
	}
	if response := sendCaptureServiceRequest(t, socket, request); !response.OK {
		t.Fatalf("valid local lease was rejected: %+v", response)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("service did not start the helper")
	}
	if response := sendCaptureServiceRequest(t, socket, request); response.OK || !strings.Contains(response.Error, "已有") {
		t.Fatalf("concurrent TUN session was accepted: %+v", response)
	}
	close(stopRun)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCaptureServiceRejectsOtherUser(t *testing.T) {
	socket := captureTestSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveCaptureService(ctx, socket, os.Geteuid()+1, false, func(string, string) error {
			t.Error("unauthorized helper launched")
			return nil
		}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("service did not start: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("service did not start")
	}
	request := system.CaptureServiceRequest{Protocol: system.CaptureServiceProtocol, URL: "http://127.0.0.1:12345", Token: strings.Repeat("a", 64)}
	if response := sendCaptureServiceRequest(t, socket, request); response.OK || !strings.Contains(response.Error, "没有") {
		t.Fatalf("other UID was accepted: %+v", response)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
