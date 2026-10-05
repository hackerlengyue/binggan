// Package config owns process configuration. No frontend build tool starts this service.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Root, DataDir, Host, Token, AppMD5 string
	Port                               int
	MaxUploadBytes                     int64
}

func Load() (Config, error) {
	root := os.Getenv("SZJM_ROOT")
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Config{}, err
	}
	appMD5 := os.Getenv("BG_APP_MD5")
	if appMD5 == "" {
		appMD5 = os.Getenv("SZJM_APP_MD5")
	}
	c := Config{Root: root, Host: "127.0.0.1", Port: 18766, MaxUploadBytes: 20 << 30, Token: os.Getenv("CAPTURE_TOKEN"), AppMD5: appMD5}
	c.DataDir = os.Getenv("CAPTURE_DATA_DIR")
	if c.DataDir == "" {
		c.DataDir = filepath.Join(root, "data")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	if err != nil {
		return c, err
	}
	if v := os.Getenv("CAPTURE_HOST"); v != "" {
		c.Host = v
	}
	if v := os.Getenv("CAPTURE_PORT"); v != "" {
		c.Port, err = strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("CAPTURE_PORT 无效")
		}
	}
	if v := os.Getenv("MAX_UPLOAD_BYTES"); v != "" {
		c.MaxUploadBytes, err = strconv.ParseInt(v, 10, 64)
		if err != nil || c.MaxUploadBytes < 1 {
			return c, fmt.Errorf("MAX_UPLOAD_BYTES 无效")
		}
	}
	return c, ValidateAddress(c.Host, c.Port)
}
func ValidateAddress(host string, port int) error {
	if host != "localhost" && net.ParseIP(host) == nil {
		return fmt.Errorf("监听地址须为 IP 地址或 localhost")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口须为 1–65535 的整数")
	}
	return nil
}
func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func LoopbackHost(host string) string {
	if host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return strings.Trim(host, "[]")
}
