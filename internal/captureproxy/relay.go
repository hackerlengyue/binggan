package captureproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

// Relay receives TCP and UDP from the desktop TUN. Only relevant HTTP(S) is
// decoded by Proxy; unrelated TLS is relayed without replacing its certificate.
type Relay struct {
	listener               *trackedListener
	Address                string
	proxyAddress, password string
	dial                   func(context.Context, string, string) (net.Conn, error)
}
type credentials string

func (c credentials) Valid(user, password, addr string) bool {
	return user == "sz-capture" && subtle.ConstantTimeCompare([]byte(password), []byte(c)) == 1
}
func NewRelay(address, proxyAddress, password string) (*Relay, error) {
	l, e := net.Listen("tcp", address)
	if e != nil {
		return nil, e
	}
	r := &Relay{listener: &trackedListener{Listener: l, connections: map[*trackedConn]bool{}}, Address: l.Addr().String(), proxyAddress: proxyAddress, password: password, dial: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext}
	s := socks5.NewServer(socks5.WithCredential(credentials(password)), socks5.WithConnectHandle(r.connect), socks5.WithDial(func(ctx context.Context, network, address string) (net.Conn, error) {
		return r.dial(ctx, network, address)
	}), socks5.WithBindIP(net.ParseIP("127.0.0.1")), socks5.WithUseBindIpBaseResolveAsUdpAddr(true))
	go s.Serve(r.listener)
	return r, nil
}
func (r *Relay) Close() { r.listener.Close(); r.listener.closeConnections() }
func (r *Relay) connect(ctx context.Context, writer io.Writer, request *socks5.Request) error {
	client, ok := writer.(net.Conn)
	if !ok {
		return fmt.Errorf("invalid relay connection")
	}
	host, port, e := net.SplitHostPort(request.RawDestAddr.String())
	if e != nil {
		return e
	}
	useProxy := TargetHost(host) || port == "80"
	if e = socks5.SendReply(writer, statute.RepSuccess, &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}); e != nil {
		return e
	}
	source := io.Reader(request.Reader)
	if !useProxy && (port == "443" || port == "8443") {
		var prefix []byte
		var name string
		name, prefix = peekServerName(client, source)
		source = io.MultiReader(bytes.NewReader(prefix), source)
		useProxy = TargetHost(name)
	}
	var target net.Conn
	if useProxy {
		target, e = r.proxyConnect(ctx, request.RawDestAddr.String())
	} else {
		target, e = r.dial(ctx, "tcp", request.RawDestAddr.String())
	}
	if e != nil {
		return e
	}
	defer target.Close()
	done := make(chan error, 2)
	go func() {
		_, e := io.Copy(target, source)
		if cw, ok := target.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
		target.SetReadDeadline(time.Now().Add(30 * time.Second))
		if e != nil {
			target.Close()
		}
		done <- e
	}()
	go func() { _, e := io.Copy(writer, target); client.Close(); done <- e }()
	e = <-done
	other := <-done
	if e != nil {
		return e
	}
	return other

}
func (r *Relay) proxyConnect(ctx context.Context, address string) (net.Conn, error) {
	c, e := r.dial(ctx, "tcp", r.proxyAddress)
	if e != nil {
		return nil, e
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	auth := base64.StdEncoding.EncodeToString([]byte("sz-capture:" + r.password))
	_, e = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", address, address, auth)
	if e != nil {
		c.Close()
		return nil, e
	}
	reader := bufio.NewReader(c)
	response, e := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if e != nil {
		c.Close()
		return nil, e
	}
	if response.StatusCode != 200 {
		c.Close()
		return nil, fmt.Errorf("capture proxy returned %d", response.StatusCode)
	}
	c.SetDeadline(time.Time{})
	return &readerConn{Conn: c, reader: reader}, nil
}

type readerConn struct {
	net.Conn
	reader io.Reader
}

func (c *readerConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *readerConn) CloseWrite() error {
	if v, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return v.CloseWrite()
	}
	return nil
}

type helloConn struct {
	net.Conn
	reader io.Reader
}

func (c *helloConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *helloConn) Write([]byte) (int, error)  { return 0, io.ErrClosedPipe }
func peekServerName(conn net.Conn, reader io.Reader) (string, []byte) {
	// crypto/tls parses ClientHello; this read-only connection never sends a TLS
	// response. Every consumed byte is replayed to the chosen upstream afterwards.
	var prefix bytes.Buffer
	var name string
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	probe := &helloConn{Conn: conn, reader: io.TeeReader(io.LimitReader(reader, 64<<10), &prefix)}
	tls.Server(probe, &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		name = strings.ToLower(h.ServerName)
		return nil, fmt.Errorf("ClientHello inspected")
	}}).Handshake()
	return name, prefix.Bytes()
}
