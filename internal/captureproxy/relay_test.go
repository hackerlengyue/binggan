package captureproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	xproxy "golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRelayTLSSelectiveDecryptionAndAuth(t *testing.T) {
	for _, target := range []bool{false, true} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			p, ca, s := setupProxy(t)
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "fixture") }))
			defer up.Close()
			upstream := strings.TrimPrefix(up.URL, "https://")
			direct := (&net.Dialer{Timeout: time.Second}).DialContext
			p.Transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return direct(ctx, network, upstream)
			}
			realRoots := x509.NewCertPool()
			realRoots.AddCert(up.Certificate())
			p.Transport.TLSClientConfig = &tls.Config{RootCAs: realRoots, ServerName: "example.com"}
			relay, e := NewRelay("127.0.0.1:0", p.Server.Addr, "fixture-secret")
			if e != nil {
				t.Fatal(e)
			}
			defer relay.Close()
			relay.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				if address == p.Server.Addr {
					return direct(ctx, network, address)
				}
				return direct(ctx, network, upstream)
			}
			rejected, _ := xproxy.SOCKS5("tcp", relay.Address, &xproxy.Auth{User: "sz-capture", Password: "wrong"}, &net.Dialer{Timeout: time.Second})
			if c, e := rejected.Dial("tcp", "203.0.113.1:443"); e == nil {
				c.Close()
				t.Fatal("invalid password accepted")
			}
			d, _ := xproxy.SOCKS5("tcp", relay.Address, &xproxy.Auth{User: "sz-capture", Password: "fixture-secret"}, &net.Dialer{Timeout: time.Second})
			wire, e := d.Dial("tcp", "203.0.113.1:443")
			if e != nil {
				t.Fatal(e)
			}
			defer wire.Close()
			wire.SetDeadline(time.Now().Add(5 * time.Second))
			name := "example.com"
			roots := realRoots
			if target {
				name = "learn.shenzaokeji.com"
				roots = x509.NewCertPool()
				roots.AddCert(ca.Certificate.Leaf)
			}
			secure := tls.Client(wire, &tls.Config{ServerName: name, RootCAs: roots})
			if e = secure.Handshake(); e != nil {
				t.Fatal(e)
			}
			if !target && !bytes.Equal(secure.ConnectionState().PeerCertificates[0].Raw, up.Certificate().Raw) {
				t.Fatal("unrelated server certificate was replaced")
			}
			fmt.Fprintf(secure, "GET /key HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", name)
			response, e := http.ReadResponse(bufio.NewReader(secure), &http.Request{Method: "GET"})
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(response.Body)
			response.Body.Close()
			secure.Close()
			if e != nil || !strings.Contains(string(b), "fixture") {
				t.Fatal("relay changed response", e, string(b))
			}
			if target {
				receive(t, s)
			} else {
				select {
				case <-s.pairs:
					t.Fatal("unrelated traffic captured")
				default:
				}
			}
		})
	}
}
func TestRelayUDPAssociate(t *testing.T) {
	echo, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 2048)
		n, a, e := echo.ReadFrom(b)
		if e == nil {
			echo.WriteTo(b[:n], a)
		}
	}()
	relay, e := NewRelay("127.0.0.1:0", "127.0.0.1:1", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	defer relay.Close()
	c, e := net.Dial("tcp", relay.Address)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	io.ReadFull(c, reply)
	if !bytes.Equal(reply, []byte{5, 2}) {
		t.Fatal(reply)
	}
	auth := append([]byte{1, 10}, []byte("sz-capture")...)
	auth = append(auth, 7)
	auth = append(auth, []byte("fixture")...)
	c.Write(auth)
	io.ReadFull(c, reply)
	if reply[1] != 0 {
		t.Fatal(reply)
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	r := make([]byte, 10)
	if _, e = io.ReadFull(c, r); e != nil || r[1] != 0 {
		t.Fatal(e, r)
	}
	dest := &net.UDPAddr{IP: net.IP(r[4:8]), Port: int(binary.BigEndian.Uint16(r[8:10]))}
	udp, e := net.DialUDP("udp", nil, dest)
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(3 * time.Second))
	port := echo.LocalAddr().(*net.UDPAddr).Port
	b := []byte{0, 0, 0, 1, 127, 0, 0, 1, byte(port >> 8), byte(port)}
	b = append(b, []byte("udp-fixture")...)
	udp.Write(b)
	out := make([]byte, 2048)
	n, e := udp.Read(out)
	if e != nil || !bytes.Equal(out[10:n], []byte("udp-fixture")) {
		t.Fatal("UDP relay failed", e)
	}
}
