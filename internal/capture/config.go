package capture

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

type Config struct {
	ReceiverURL string `json:"receiverUrl"`
	Token       string `json:"token"`
}

func DefaultConfig() Config { return Config{ReceiverURL: "http://127.0.0.1:18768"} }
func (c Config) Validate() error {
	u, e := url.Parse(c.ReceiverURL)
	if e != nil || u == nil {
		return fmt.Errorf("接收地址无效")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("请填写本机后端地址，例如 http://127.0.0.1:18768")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("接收地址只需填写协议、主机和端口")
	}
	if len(c.Token) > 512 {
		return fmt.Errorf("令牌过长")
	}
	return nil
}
func IsLocalURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	host, port, e := net.SplitHostPort(u.Host)
	n, pe := strconv.Atoi(port)
	return e == nil && pe == nil && host == "127.0.0.1" && n > 0 && n <= 65535
}
