package surge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Opt-in: briefly installs the same narrowly scoped rule in the running Surge,
// then restores the profile. It sends no traffic to the real player or service.
func TestNativeSurgeRoutesOnlyShenzaoAndRestores(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("BINGGAN_SURGE_INTEGRATION") != "1" {
		t.Skip("requires explicit native Surge integration run")
	}
	ctx := context.Background()
	active, err := Active(ctx)
	if err != nil || !active {
		t.Fatalf("active Surge required: %v", err)
	}
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := m.current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path, err := m.profilePath(profile)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Disable(ctx); err != nil {
			t.Error("restore failed", err)
		}
		current, _ := os.ReadFile(path)
		if string(current) != string(original) {
			t.Error("native profile did not restore byte-for-byte")
		}
	})
	match := func(host, process, protocol string) string {
		t.Helper()
		b, err := m.run(ctx, "--raw", "rule", "match", host, "process-path="+process, "protocol="+protocol)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Policy string `json:"policy"`
		}
		if json.Unmarshal(b, &v) != nil || v.Policy == "" {
			t.Fatal("no routing policy")
		}
		return v.Policy
	}
	d := descriptor(t)
	const domain = "binggan-coexist-probe.shenzaokeji.com"
	const process = "/Applications/SzPlayer.app/Contents/MacOS/SzPlayer"
	others := []string{"example.com", "www.apple.com", "shenzaokeji.com.example.com", "notshenzaokeji.com"}
	before := map[string]string{}
	for _, host := range others {
		before[host] = match(host, process, "HTTPS")
	}
	backendPolicy := match("www.shenzaokeji.com", d.BackendExecutable, "HTTPS")
	if match("www.shenzaokeji.com", process, "HTTPS") == policyName {
		t.Fatal("probe requires a clean initial rule set")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	d.ProxyAddress = listener.Addr().String()
	delivered := make(chan string, 4)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				r := bufio.NewReader(c)
				read := func(n int) []byte {
					b := make([]byte, n)
					if _, err := io.ReadFull(r, b); err != nil {
						return nil
					}
					return b
				}
				header := read(2)
				if len(header) != 2 || header[0] != 5 {
					return
				}
				if len(read(int(header[1]))) == 0 {
					return
				}
				c.Write([]byte{5, 2})
				header = read(2)
				if len(header) != 2 || header[0] != 1 {
					return
				}
				username := string(read(int(header[1])))
				length := read(1)
				if len(length) != 1 {
					return
				}
				password := string(read(int(length[0])))
				if username != d.ProxyUsername || password != d.ProxyPassword {
					c.Write([]byte{1, 1})
					return
				}
				c.Write([]byte{1, 0})
				header = read(5)
				if len(header) != 5 || header[0] != 5 || header[1] != 1 || header[3] != 3 {
					return
				}
				host := string(read(int(header[4])))
				if len(read(2)) != 2 || host != domain {
					return
				}
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				request, err := http.ReadRequest(r)
				if err != nil {
					return
				}
				request.Body.Close()
				delivered <- host
				const body = "binggan-native-surge-ok"
				fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}()
		}
	}()
	if err = m.Enable(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"shenzaokeji.com", "www.shenzaokeji.com", domain} {
		for _, protocol := range []string{"TCP", "HTTP", "HTTPS"} {
			if got := match(host, process, protocol); got != policyName {
				t.Fatalf("target %s (%s) was not forwarded: %s", host, protocol, got)
			}
		}
	}
	for _, host := range others {
		if match(host, process, "HTTPS") != before[host] {
			t.Errorf("unrelated routing changed: %s", host)
		}
	}
	if match("www.shenzaokeji.com", d.BackendExecutable, "HTTPS") != backendPolicy {
		t.Fatal("backend upstream would loop")
	}
	if match(domain, process, "UDP") == policyName {
		t.Fatal("UDP must retain original routing")
	}
	proxyAddress := os.Getenv("BINGGAN_SURGE_HTTP_PROXY")
	if proxyAddress == "" {
		proxyAddress = "http://127.0.0.1:6152"
	}
	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Hostname() != "127.0.0.1" {
		t.Fatal("integration proxy must be loopback")
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
	response, err := client.Get("http://" + domain + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	response.Body.Close()
	if err != nil || strings.TrimSpace(string(body)) != "binggan-native-surge-ok" {
		t.Fatal("request did not reach authenticated local capture transport")
	}
	select {
	case got := <-delivered:
		if got != domain {
			t.Fatal("wrong target")
		}
	default:
		t.Fatal("local relay never received request")
	}
	if err = m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if match("www.shenzaokeji.com", process, "HTTPS") == policyName {
		t.Fatal("rule survived capture stop")
	}
}
