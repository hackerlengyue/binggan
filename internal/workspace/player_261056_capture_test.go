package workspace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	box "github.com/sagernet/sing-box"
	M "github.com/sagernet/sing/common/metadata"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/tunnel"
)

// Exercise the real sing-box SOCKS outbound, authenticated relay, TLS MITM and
// durable collector together. No TUN is started and no system routes change.
func TestPlayer261056SingboxCaptureToHistory(t *testing.T) {
	app := testApp(t)
	app.collector.mu.Lock()
	err := app.collector.startLocked(0)
	app.collector.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	app.SetCaptureState(true)
	bodies := map[string]string{
		"/api/courseLocal/getPerinutePwd": `{"data":{"pwdMaps":{"第一个密码":{"密码":"synthetic-aes-cipher"}},"pattern":"3","pmn":2,"whenLong":10}}`,
		"/api/courseLocal/getPwd3V2":      `{"data":{"pwd":"abcdef0123456789abcdef0123456789"}}`,
	}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("SoftwareName") != "MAC_sz_26.10.56" || r.Header.Get("CurrentToken") != "synthetic-token" {
			t.Error("player headers changed before upstream")
		}
		w.Header().Set("Content-Encoding", "br")
		writer := brotli.NewWriter(w)
		_, _ = io.WriteString(writer, bodies[r.URL.Path])
		_ = writer.Close()
	}))
	defer upstream.Close()
	proxy := app.collector.proxy
	proxy.Transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, strings.TrimPrefix(upstream.URL, "https://"))
	}
	upstreamRoots := x509.NewCertPool()
	upstreamRoots.AddCert(upstream.Certificate())
	proxy.Transport.TLSClientConfig = &tls.Config{RootCAs: upstreamRoots, ServerName: "example.com", MinVersion: tls.VersionTLS12}
	descriptor, err := app.CollectorDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	var d capture.Descriptor
	raw, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	ctx, opts, err := tunnel.Options(d)
	if err != nil {
		t.Fatal(err)
	}
	// Keep production outbounds and credentials. Disable only privileged ingress
	// and OS process routing, which are covered by the tunnel matcher tests.
	opts.Inbounds = nil
	opts.Route = nil
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err = instance.Start(); err != nil {
		t.Fatal(err)
	}
	outbound, ok := instance.Outbound().Outbound("capture")
	if !ok {
		t.Fatal("capture outbound missing")
	}
	roots := x509.NewCertPool()
	roots.AddCert(app.collector.ca.Certificate.Leaf)
	for i, path := range []string{"/api/courseLocal/getPerinutePwd", "/api/courseLocal/getPwd3V2"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		wire, err := outbound.DialContext(ctx, "tcp", M.ParseSocksaddr("video.shenzaokeji.com:443"))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		_ = wire.SetDeadline(time.Now().Add(5 * time.Second))
		secure := tls.Client(wire, &tls.Config{RootCAs: roots, ServerName: "video.shenzaokeji.com", MinVersion: tls.VersionTLS12})
		body := `{"den":10,"dts":120,"num":1}`
		if i == 0 {
			body = `{"startTime":"1","endTime":"1","encryptionMd5":"synthetic-file","softMd5":"synthetic-soft","userId":"synthetic-user"}`
		}
		req, err := http.NewRequest("POST", "https://video.shenzaokeji.com"+path, strings.NewReader(body))
		if err != nil {
			secure.Close()
			cancel()
			t.Fatal(err)
		}
		req.Close = true
		req.Header.Set("SoftwareName", "MAC_sz_26.10.56")
		req.Header.Set("CurrentToken", "synthetic-token")
		req.Header.Set("Uit", []string{"synthetic-uit-one", "synthetic-uit-two"}[i])
		if err = req.Write(secure); err != nil {
			secure.Close()
			cancel()
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(secure), req)
		if err != nil {
			secure.Close()
			cancel()
			t.Fatal(err)
		}
		compressed, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		secure.Close()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(compressed)))
		if err != nil || string(decoded) != bodies[path] {
			t.Fatal("player response changed", err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	var events []Capture
	for time.Now().Before(deadline) {
		events, err = app.History(100)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 4 {
		t.Fatalf("captured %d events, want two complete pairs", len(events))
	}
	pairs := map[string][]Capture{}
	for _, event := range events {
		if event.Source != "sing-box" || event.RequestID == "" || event.BodyTruncated {
			t.Fatal("invalid durable event")
		}
		pairs[event.RequestID] = append(pairs[event.RequestID], event)
		if event.Phase == "request" && (event.Headers["Softwarename"] != "MAC_sz_26.10.56" || event.Headers["Uit"] == "" || event.Headers["Currenttoken"] != "synthetic-token") {
			t.Fatal("required capture headers missing")
		}
		if event.Phase == "response" {
			path := strings.TrimPrefix(event.URL, "https://video.shenzaokeji.com")
			if event.Body != bodies[path] {
				t.Fatal("compressed response was not decoded into history")
			}
		}
	}
	if len(pairs) != 2 {
		t.Fatal("request correlation lost")
	}
	for _, pair := range pairs {
		if len(pair) != 2 || pair[0].Phase == pair[1].Phase {
			t.Fatal("incomplete correlated pair")
		}
	}
}
