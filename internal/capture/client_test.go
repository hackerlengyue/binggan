package capture

import (
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
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) Descriptor {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(der)
	return Descriptor{Version: 2, BackendExecutable: filepath.Join(t.TempDir(), "szjm-server"), Ready: true, ProxyAddress: "127.0.0.1:18767", ProxyUsername: "sz-capture", ProxyPassword: hex.EncodeToString(make([]byte, 32)), Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Fingerprint: hex.EncodeToString(sum[:])}
}
func TestDescriptorAndControlAddressValidation(t *testing.T) {
	d := fixture(t)
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Descriptor){func(v *Descriptor) { v.Fingerprint = "bad" }, func(v *Descriptor) { v.ProxyAddress = "remote.example:80" }, func(v *Descriptor) { v.ProxyAddress = "127.0.0.1:0" }, func(v *Descriptor) { v.ProxyPassword = "invalid" }} {
		v := d
		change(&v)
		if v.Validate() == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
	for _, u := range []string{"http://127.0.0.1:80.evil", "http://127.0.0.1:0", "http://127.0.0.1:8080/path", "http://127.0.0.1:8080@evil", "https://127.0.0.1:80"} {
		if IsLocalURL(u) {
			t.Fatal("unsafe helper URL", u)
		}
	}
	if !IsLocalURL("http://127.0.0.1:12345") {
		t.Fatal("valid helper URL rejected")
	}
}
func TestLocalClientDisablesProxyAndRedirects(t *testing.T) {
	d := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-capture-token") != "fixture" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	client := LocalClient()
	defer client.CloseIdleConnections()
	if _, e := Fetch(context.Background(), client, Config{ReceiverURL: server.URL, Token: "fixture"}); e != nil {
		t.Fatal(e)
	}
	if _, e := Fetch(context.Background(), client, Config{ReceiverURL: server.URL}); e == nil {
		t.Fatal("unauthorized accepted")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, 302) }))
	defer redirect.Close()
	if _, e := Fetch(context.Background(), client, Config{ReceiverURL: redirect.URL, Token: "fixture"}); e == nil {
		t.Fatal("redirect followed")
	}
}
func TestRegenerateCertificateRequest(t *testing.T) {
	next := fixture(t)
	var method, path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		json.NewEncoder(w).Encode(next)
	}))
	defer server.Close()
	client := LocalClient()
	defer client.CloseIdleConnections()
	d, e := Regenerate(context.Background(), client, Config{ReceiverURL: server.URL, Token: "fixture"})
	if e != nil || method != http.MethodPost || path != "/api/collector/certificate" || d.Fingerprint != next.Fingerprint {
		t.Fatal(method, path, e, d.Fingerprint)
	}
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	_, e = Regenerate(context.Background(), client, Config{ReceiverURL: missing.URL})
	if e == nil || !strings.Contains(e.Error(), "重新打开软件") {
		t.Fatal(e)
	}
}
