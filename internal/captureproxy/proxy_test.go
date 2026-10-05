package captureproxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/andybalholm/brotli"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testSink struct {
	enabled atomic.Bool
	pairs   chan Pair
}

func (s *testSink) Enabled() bool              { return s.enabled.Load() }
func (s *testSink) Accept(p Pair)              { s.pairs <- p }
func (s *testSink) Log(string, string, string) {}
func setupProxy(t *testing.T) (*Proxy, CA, *testSink) {
	t.Helper()
	ca, e := LoadCA(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s := &testSink{pairs: make(chan Pair, 16)}
	s.enabled.Store(true)
	p := NewProxy("127.0.0.1:0", ca, s)
	if e = p.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p, ca, s
}
func receive(t *testing.T, s *testSink) Pair {
	t.Helper()
	select {
	case p := <-s.pairs:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("capture missing")
		return Pair{}
	}
}
func TestTLSOverIPConnectPreservesPayloadAndCorrelatesHost(t *testing.T) {
	p, ca, s := setupProxy(t)
	body := `{"data":{"key":"synthetic-fixture"}}`
	var compressed bytes.Buffer
	bw := brotli.NewWriter(&compressed)
	bw.Write([]byte(body))
	bw.Close()
	input := strings.Repeat("x", bodyLimit+200)
	got := make(chan string, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
		w.Header().Set("Content-Encoding", "br")
		w.Header().Set("UIT", "fixture-uit")
		w.Header().Set("Set-Cookie", "secret")
		w.Write(compressed.Bytes())
	}))
	defer upstream.Close()
	p.Transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "https://"))
	}
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	p.Transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	c, e := net.DialTimeout("tcp", p.Server.Addr, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(c, "CONNECT 203.0.113.1:443 HTTP/1.1\r\nHost: 203.0.113.1:443\r\n\r\n")
	resp, e := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: "CONNECT"})
	if e != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", resp, e)
	}
	trusted := x509.NewCertPool()
	trusted.AddCert(ca.Certificate.Leaf)
	secure := tls.Client(c, &tls.Config{RootCAs: trusted, ServerName: "learn.shenzaokeji.com", MinVersion: tls.VersionTLS12})
	if e = secure.Handshake(); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("POST", "https://learn.shenzaokeji.com/api/key", strings.NewReader(input))
	req.Header.Set("Cookie", "private")
	req.Header.Set("UIT", "fixture")
	req.Close = true
	if e = req.Write(secure); e != nil {
		t.Fatal(e)
	}
	resp, e = http.ReadResponse(bufio.NewReader(secure), req)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || !bytes.Equal(b, compressed.Bytes()) {
		t.Fatal("response bytes changed", e)
	}
	if v := <-got; v != input {
		t.Fatal("request body changed")
	}
	pair := receive(t, s)
	if pair.Response.Body != body || pair.Response.BodyTruncated || !pair.Request.BodyTruncated || pair.Request.Host != "learn.shenzaokeji.com" || pair.Response.Headers["Uit"] != "fixture-uit" {
		t.Fatalf("bad capture: request truncated %v, response %#v", pair.Request.BodyTruncated, pair.Response)
	}
	if pair.Request.Headers["Cookie"] != "" || pair.Response.Headers["Set-Cookie"] != "" {
		t.Fatal("sensitive headers logged")
	}
}
func TestHTTPCapturePauseAndNonTarget(t *testing.T) {
	p, _, s := setupProxy(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fixture") }))
	defer upstream.Close()
	p.Transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "http://"))
	}
	u, _ := url.Parse("http://" + p.Server.Addr)
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	get := func(host string) {
		t.Helper()
		r, e := client.Get("http://" + host + "/api/test")
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if string(b) != "fixture" {
			t.Fatal(string(b))
		}
	}
	get("learn.shenzaokeji.com")
	if v := receive(t, s); v.Response.BodyTruncated {
		t.Fatal("complete response marked truncated")
	}
	s.enabled.Store(false)
	get("learn.shenzaokeji.com")
	s.enabled.Store(true)
	get("example.com")
	select {
	case <-s.pairs:
		t.Fatal("captured disabled/unrelated request")
	default:
	}
}
func TestRejectsUntrustedUpstreamTLS(t *testing.T) {
	p, _, s := setupProxy(t)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted upstream accepted") }))
	defer up.Close()
	p.Transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(up.URL, "https://"))
	}
	if p.Transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
	r, e := p.Transport.RoundTrip(&http.Request{Method: "GET", URL: &url.URL{Scheme: "https", Host: "learn.shenzaokeji.com", Path: "/"}, Header: make(http.Header)})
	if r != nil {
		r.Body.Close()
	}
	if e == nil {
		t.Fatal("untrusted server accepted")
	}
	select {
	case <-s.pairs:
		t.Fatal("unexpected capture")
	default:
	}
}
func TestDecodeLimitsAndCertificatePersistence(t *testing.T) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(bytes.Repeat([]byte("a"), bodyLimit+1))
	w.Close()
	value, cut := decodeBody(b.Bytes(), "gzip", false)
	if !cut || len(value) != bodyLimit {
		t.Fatal("decompression size limit failed")
	}
	if _, cut = decodeBody([]byte("invalid"), "gzip", false); !cut {
		t.Fatal("invalid compressed body accepted")
	}
	if _, cut = decodeBody([]byte{0xff}, "", false); !cut {
		t.Fatal("binary body accepted")
	}
	dir := t.TempDir()
	a, e := LoadCA(dir)
	if e != nil {
		t.Fatal(e)
	}
	a2, e := LoadCA(dir)
	if e != nil || a.Fingerprint != a2.Fingerprint {
		t.Fatal("CA was not reused", e)
	}
	replaced, e := ReplaceCA(dir)
	if e != nil || replaced.Fingerprint == "" || replaced.Fingerprint == a.Fingerprint {
		t.Fatal("CA was not replaced", e)
	}
	again, e := LoadCA(dir)
	if e != nil || again.Fingerprint != replaced.Fingerprint {
		t.Fatal("replaced CA was not saved", e)
	}
}
