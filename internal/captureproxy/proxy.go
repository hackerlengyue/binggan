package captureproxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"crypto/tls"
	"fmt"
	"github.com/andybalholm/brotli"
	"github.com/elazarl/goproxy"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const bodyLimit = 256 << 10

type Proxy struct {
	Server    *http.Server
	Transport *http.Transport
	listener  *trackedListener
	Sink      Sink
}
type certificateCache struct {
	mu      sync.Mutex
	entries map[string]*tls.Certificate
}

func (c *certificateCache) Fetch(host string, gen func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v := c.entries[host]; v != nil {
		return v, nil
	}
	v, e := gen()
	if e == nil {
		if len(c.entries) >= 128 {
			clear(c.entries)
		}
		c.entries[host] = v
	}
	return v, e
}

type proxyLogger struct{ sink Sink }

func (l proxyLogger) Printf(format string, args ...any) {
	// goproxy's verbose messages can contain full URLs. Keep its diagnostics useful
	// without writing captured credentials or query values to the application log.
	raw := fmt.Sprintf(format, args...)
	message := "代理连接失败，请检查网络和目标接口的可用性"
	if strings.Contains(strings.ToLower(raw), "tls") || strings.Contains(strings.ToLower(raw), "certificate") {
		message = "HTTPS 握手未完成，请查看证书信任、服务端证书或 APP 校验限制"
	}
	l.sink.Log("warn", message, "")
}

type requestCapture struct {
	event Event
	start time.Time
	path  string
}

func NewProxy(address string, ca CA, sink Sink) *Proxy {
	p := goproxy.NewProxyHttpServer()
	p.Logger = proxyLogger{sink}
	p.AllowHTTP2 = true
	p.CertStore = &certificateCache{entries: make(map[string]*tls.Certificate)}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	tr.ResponseHeaderTimeout = 30 * time.Second
	tr.MaxIdleConns = 32
	tr.MaxIdleConnsPerHost = 8
	p.Tr = tr
	p.ConnectDial = nil
	p.ConnectDialWithReq = func(req *http.Request, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(req.Context(), network, address)
	}
	tlsConfig := goproxy.TLSConfigFromCA(&ca.Certificate)
	p.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		name, port, err := net.SplitHostPort(host)
		if err != nil {
			name, port = host, "443"
		}
		sink.Log("info", "目标软件连接已进入抓包入口", net.JoinHostPort(name, port))
		if port == "80" {
			return &goproxy.ConnectAction{Action: goproxy.ConnectMitm}, host
		}
		if port == "443" || port == "8443" || TargetHost(name) {
			return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: func(h string, c *goproxy.ProxyCtx) (*tls.Config, error) {
				// TUN often forwards an IP in CONNECT. Use the actual ClientHello
				// server name so the certificate still matches the APP's host.
				return &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
					server := h
					if hello.ServerName != "" {
						server = hello.ServerName
					}
					cfg, e := tlsConfig(server, c)
					if e == nil {
						cfg.MinVersion = tls.VersionTLS12
						cfg.InsecureSkipVerify = false
						cfg.NextProtos = []string{"h2", "http/1.1"}
					}
					return cfg, e
				}}, nil
			}}, host
		}
		return goproxy.OkConnect, host
	})
	p.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if !TargetHost(req.URL.Hostname()) || !sink.Enabled() {
			return req, nil
		}
		start := time.Now()
		headers := captureHeaders(req.Header)
		body, truncated, e := peekBody(req)
		if e != nil {
			truncated = true
		}
		ctx.UserData = &requestCapture{Event{Phase: "request", TS: start.UTC().Format(time.RFC3339Nano), Method: req.Method, URL: req.URL.String(), Host: strings.ToLower(req.URL.Hostname()), Headers: headers, Body: body, BodyTruncated: truncated, Source: "sing-box"}, start, req.URL.Path}
		return req, nil
	})
	p.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		captured, ok := ctx.UserData.(*requestCapture)
		if !ok {
			return resp
		}
		if resp == nil {
			sink.Log("warn", "目标接口请求失败，未收到响应", captured.path)
			return resp
		}
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return resp
		}
		event := captured.event
		event.Phase = "response"
		event.Status = resp.StatusCode
		event.Headers = captureHeaders(resp.Header)
		event.Headers["x-duration-ms"] = strconv.FormatInt(time.Since(captured.start).Milliseconds(), 10)
		encoding := resp.Header.Get("Content-Encoding")
		resp.Body = &tapBody{ReadCloser: resp.Body, done: func(b []byte, truncated bool) {
			event.Body, event.BodyTruncated = decodeBody(b, encoding, truncated)
			if captured.event.BodyTruncated || event.BodyTruncated {
				sink.Log("warn", "接口内容不完整，网页会阻止导出不完整密钥", captured.path)
			}
			sink.Accept(Pair{captured.event, event, captured.path})
		}}
		return resp
	})
	p.NonproxyHandler = http.NotFoundHandler()
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "Local connections only", 403)
			return
		}
		// Do not accept requests that would send the proxy back into itself.
		if r.Method == http.MethodConnect && r.Host == address || r.URL.IsAbs() && r.URL.Host == address {
			http.Error(w, "Proxy loop rejected", 400)
			return
		}
		p.ServeHTTP(w, r)
	})
	return &Proxy{Server: &http.Server{Addr: address, Handler: wrapper, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}, Transport: tr, Sink: sink}
}
func (p *Proxy) Start() error {
	l, e := net.Listen("tcp", p.Server.Addr)
	if e != nil {
		return fmt.Errorf("无法监听 %s，端口可能已被占用：%w", p.Server.Addr, e)
	}
	p.Server.Addr = l.Addr().String()
	p.listener = &trackedListener{Listener: l, connections: map[*trackedConn]bool{}}
	go func() {
		if e := p.Server.Serve(p.listener); e != nil && e != http.ErrServerClosed {
			p.Sink.Log("error", "抓包服务已停止", e.Error())
		}
	}()
	return nil
}
func (p *Proxy) Close() {
	p.Server.Close()
	if p.listener != nil {
		p.listener.closeConnections()
	}
	p.Transport.CloseIdleConnections()
}
func captureHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	size := 0
	for k, v := range h {
		switch strings.ToLower(k) {
		case "authorization", "proxy-authorization", "cookie", "set-cookie":
			continue
		}
		value := strings.Join(v, ", ")
		if size+len(k)+len(value) > 64<<10 {
			continue
		}
		size += len(k) + len(value)
		out[k] = value
	}
	return out
}

type bodyReplay struct {
	io.Reader
	io.Closer
}

func peekBody(r *http.Request) (string, bool, error) {
	if r.Body == nil {
		return "", false, nil
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	r.Body = &bodyReplay{io.MultiReader(bytes.NewReader(b), r.Body), r.Body}
	cut := len(b) > bodyLimit
	if cut {
		b = b[:bodyLimit]
	}
	body, truncated := decodeBody(b, r.Header.Get("Content-Encoding"), cut || e != nil)
	return body, truncated, e
}
func decodeBody(b []byte, encoding string, truncated bool) (string, bool) {
	if truncated {
		return string(bytes.ToValidUTF8(b, []byte("�"))), true
	}
	var reader io.Reader = bytes.NewReader(b)
	var closer io.Closer
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
	case "gzip":
		g, e := gzip.NewReader(reader)
		if e != nil {
			return "", true
		}
		reader = g
		closer = g
	case "deflate":
		z, e := zlib.NewReader(reader)
		if e != nil {
			z = flate.NewReader(bytes.NewReader(b))
		}
		reader = z
		closer = z
	case "br":
		reader = brotli.NewReader(reader)
	default:
		return "", true
	}
	if closer != nil {
		defer closer.Close()
	}
	data, e := io.ReadAll(io.LimitReader(reader, bodyLimit+1))
	if len(data) > bodyLimit {
		data = data[:bodyLimit]
		truncated = true
	}
	return string(bytes.ToValidUTF8(data, []byte("�"))), truncated || e != nil || !utf8.Valid(data)
}

type tapBody struct {
	io.ReadCloser
	buf   []byte
	read  int
	ended bool
	once  sync.Once
	done  func([]byte, bool)
}

func (b *tapBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.read += n
	if left := bodyLimit - len(b.buf); left > 0 {
		b.buf = append(b.buf, p[:min(n, left)]...)
	}
	if e == io.EOF {
		b.ended = true
	}
	return n, e
}
func (b *tapBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.done(b.buf, b.read > bodyLimit || !b.ended) })
	return e
}

type trackedListener struct {
	net.Listener
	mu          sync.Mutex
	connections map[*trackedConn]bool
	closed      bool
}
type trackedConn struct {
	net.Conn
	owner *trackedListener
	once  sync.Once
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	t := &trackedConn{Conn: c, owner: l}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		c.Close()
		return nil, net.ErrClosed
	}
	l.connections[t] = true
	l.mu.Unlock()
	return t, nil
}
func (c *trackedConn) Close() error {
	e := c.Conn.Close()
	c.once.Do(func() { c.owner.mu.Lock(); delete(c.owner.connections, c); c.owner.mu.Unlock() })
	return e
}
func (l *trackedListener) closeConnections() {
	l.mu.Lock()
	l.closed = true
	items := make([]*trackedConn, 0, len(l.connections))
	for c := range l.connections {
		items = append(items, c)
	}
	l.mu.Unlock()
	for _, c := range items {
		c.Close()
	}
}
