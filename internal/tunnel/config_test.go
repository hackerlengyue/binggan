package tunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/route/rule"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time.haomen/binggan/v2/internal/capture"
	"testing"
	"time"
)

func descriptor(t *testing.T) capture.Descriptor {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Isolated capture test CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(der)
	return capture.Descriptor{Version: 2, BackendExecutable: filepath.Join(t.TempDir(), "szjm-server"), Ready: true, ProxyAddress: "127.0.0.1:18767", ProxyUsername: "sz-capture", ProxyPassword: strings.Repeat("a", 64), Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Fingerprint: hex.EncodeToString(sum[:])}
}
func TestSingboxConfigurationInstantiatesWithoutStartingNetwork(t *testing.T) {
	ctx, opts, e := Options(descriptor(t))
	if e != nil {
		t.Fatal(e)
	}
	instance, e := box.New(box.Options{Context: ctx, Options: opts})
	if e != nil {
		t.Fatal(e)
	}
	defer instance.Close()
	// Deliberately do not Start: unit tests must never create a TUN or change routes.
	if len(opts.Inbounds) != 1 || len(opts.Outbounds) != 2 || opts.Route.Final != "direct" {
		t.Fatal("unexpected routing structure")
	}
	raw, _ := ConfigJSON(descriptor(t))
	for _, v := range []string{"SzPlayer.exe", "szplayer.exe", "\"type\": \"socks\"", "\"dns_mode\": \"disabled\"", "SzPlayer"} {
		if !strings.Contains(string(raw), v) {
			t.Fatal("missing routing constraint", v)
		}
	}
}
func TestLeaseAuthenticationAndShutdown(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "logs"), 0700)
	logs := capture.NewLogbook(dir)
	defer logs.Close()
	l, e := openLease(descriptor(t), logs)
	if e != nil {
		t.Fatal(e)
	}
	defer l.server.Close()
	client := capture.LocalClient()
	defer client.CloseIdleConnections()
	r, e := client.Get(l.URL + "/lease")
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatal("lease allows unauthenticated clients")
	}
	if _, e = readLease(client, l.URL, l.Token); e != nil {
		t.Fatal(e)
	}
	report(client, l.URL, l.Token, Report{Status: "error", Message: "test failure", Level: "error"})
	report(client, l.URL, l.Token, Report{Status: "stopped", Message: "stopped"})
	if state, _ := l.State(); state != "error" {
		t.Fatal("shutdown masked original failure")
	}
	if e = l.Stop(); e != nil {
		t.Fatal(e)
	}
	if _, e = readLease(client, l.URL, l.Token); e == nil {
		t.Fatal("closed channel still readable")
	}
}

// Exercise sing-box's actual process matcher with native path separators.
func TestCaptureProcessMatchingAcrossPlatforms(t *testing.T) {
	matcher := rule.NewProcessItem(captureProcesses)
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"SzPlayer", true}, {"szplayer", true}, {"SzPlayer.exe", true}, {"szplayer.exe", true},
		{"szjm-server", false}, {"szjm-server.exe", false}, {"chrome.exe", false}, {"Safari", false}, {"SzPlayer-helper.exe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := adapter.InboundContext{ProcessInfo: &adapter.ConnectionOwner{ProcessPath: filepath.Join(t.TempDir(), tc.name)}}
			if got := matcher.Match(&metadata); got != tc.want {
				t.Fatalf("capture %q = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	if matcher.Match(&adapter.InboundContext{}) {
		t.Fatal("unknown process must stay direct")
	}
}
