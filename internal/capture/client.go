package capture

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Descriptor struct {
	BackendExecutable string  `json:"backendExecutable"`
	Version           int     `json:"version"`
	Ready             bool    `json:"ready"`
	ProxyAddress      string  `json:"proxyAddress"`
	ProxyUsername     string  `json:"proxyUsername"`
	ProxyPassword     string  `json:"proxyPassword"`
	Certificate       string  `json:"certificate"`
	Fingerprint       string  `json:"fingerprint"`
	CaptureEnabled    bool    `json:"captureEnabled"`
	RoutingRevision   uint64  `json:"routingRevision,omitempty"`
	Received          int64   `json:"received"`
	Logs              []Entry `json:"logs"`
}

func LocalClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return &http.Client{Transport: t, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func Fetch(ctx context.Context, client *http.Client, c Config) (Descriptor, error) {
	var d Descriptor
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.ReceiverURL, "/")+"/api/collector", nil)
	if e != nil {
		return d, e
	}
	req.Header.Set("x-capture-token", c.Token)
	resp, e := client.Do(req)
	if e != nil {
		return d, fmt.Errorf("接收服务离线，等待饼干大小姐启动")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return d, fmt.Errorf("后端认证未通过，请重新打开软件以更新连接信息")
	}
	if resp.StatusCode != 200 {
		return d, fmt.Errorf("后端尚未提供抓包入口（HTTP %d），请重新安装或更新软件", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&d); e != nil {
		return d, fmt.Errorf("抓包入口返回的数据格式无效")
	}
	return d, d.Validate()
}
func Regenerate(ctx context.Context, client *http.Client, c Config) (Descriptor, error) {
	var d Descriptor
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.ReceiverURL, "/")+"/api/collector/certificate", nil)
	if e != nil {
		return d, e
	}
	req.Header.Set("x-capture-token", c.Token)
	resp, e := client.Do(req)
	if e != nil {
		return d, fmt.Errorf("接收服务离线，等待饼干大小姐启动")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return d, fmt.Errorf("后端认证未通过，请重新打开软件以更新连接信息")
	}
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return d, fmt.Errorf("当前后端还不能生成新证书，请重新打开软件")
	}
	if resp.StatusCode != 200 {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
		if body.Message != "" {
			return d, fmt.Errorf("%s", body.Message)
		}
		return d, fmt.Errorf("生成新证书失败（HTTP %d）", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&d); e != nil {
		return d, fmt.Errorf("抓包入口返回的数据格式无效")
	}
	return d, d.Validate()
}
func (d Descriptor) Validate() error {
	if d.Version != 2 || !d.Ready {
		return fmt.Errorf("后端抓包入口尚未就绪")
	}
	if !filepath.IsAbs(d.BackendExecutable) {
		return fmt.Errorf("后端进程路径无效，无法设置回环保护")
	}
	host, port, e := net.SplitHostPort(d.ProxyAddress)
	if e != nil || host != "127.0.0.1" || port == "" {
		return fmt.Errorf("后端抓包地址无效")
	}
	n, pe := strconv.Atoi(port)
	if pe != nil || n < 1 || n > 65535 {
		return fmt.Errorf("抓包端口无效")
	}
	if _, e = hex.DecodeString(d.ProxyPassword); e != nil {
		return fmt.Errorf("抓包入口认证信息无效")
	}
	if d.ProxyUsername != "sz-capture" || len(d.ProxyPassword) != 64 {
		return fmt.Errorf("抓包入口认证信息不完整")
	}
	block, rest := pem.Decode([]byte(d.Certificate))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return fmt.Errorf("后端证书格式无效")
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil || !cert.IsCA || time.Now().After(cert.NotAfter) || time.Now().Before(cert.NotBefore) {
		return fmt.Errorf("后端证书无效或已过期")
	}
	sum := sha256.Sum256(block.Bytes)
	if hex.EncodeToString(sum[:]) != d.Fingerprint {
		return fmt.Errorf("后端证书指纹不匹配")
	}
	return nil
}
