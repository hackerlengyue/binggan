//go:build darwin

package system

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"time.haomen/binggan/v2/internal/capture"
)

const CaptureServiceProtocol = 1

type CaptureServiceRequest struct {
	Protocol int    `json:"protocol"`
	URL      string `json:"url"`
	Token    string `json:"token"`
}

type CaptureServiceResponse struct {
	OK              bool   `json:"ok"`
	UpgradeRequired bool   `json:"upgradeRequired,omitempty"`
	Error           string `json:"error,omitempty"`
}

var ErrCaptureServiceUpgrade = errors.New("流量接管服务需要升级")

func CaptureServiceSocket(uid int) string {
	return fmt.Sprintf("/var/run/local.szjm.capture.v2.%d.sock", uid)
}

func captureServiceLabel(uid int) string {
	return fmt.Sprintf("local.szjm.capture.v2.%d", uid)
}

func captureServiceBinary(uid int) string {
	return filepath.Join("/Library/PrivilegedHelperTools", captureServiceLabel(uid))
}

func captureServicePlist(uid int) string {
	return filepath.Join("/Library/LaunchDaemons", captureServiceLabel(uid)+".plist")
}

func bundledCaptureService(executable string) string {
	return filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "Resources", "tools", "capture-service"))
}

func ValidCaptureServiceRequest(req CaptureServiceRequest) bool {
	if req.Protocol != CaptureServiceProtocol || !capture.IsLocalURL(req.URL) || len(req.Token) != 64 {
		return false
	}
	token, err := hex.DecodeString(req.Token)
	return err == nil && len(token) == 32
}

// requestCaptureService returns false only when no daemon is listening.
func requestCaptureService(ctx context.Context, uid int, address, token string) (bool, error) {
	req := CaptureServiceRequest{Protocol: CaptureServiceProtocol, URL: address, Token: token}
	if !ValidCaptureServiceRequest(req) {
		return true, fmt.Errorf("无效的本机流量接管请求")
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", CaptureServiceSocket(uid))
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("连接流量接管服务失败：%w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return true, err
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return true, fmt.Errorf("发送流量接管请求失败：%w", err)
	}
	var response CaptureServiceResponse
	if err := json.NewDecoder(io.LimitReader(conn, 2048)).Decode(&response); err != nil {
		return true, fmt.Errorf("读取流量接管服务响应失败：%w", err)
	}
	if !response.OK {
		if response.UpgradeRequired {
			return true, ErrCaptureServiceUpgrade
		}
		if response.Error == "" {
			response.Error = "流量接管服务拒绝了请求"
		}
		return true, errors.New(response.Error)
	}
	return true, nil
}

func installCaptureServicePlist(uid int) []byte {
	label := captureServiceLabel(uid)
	binary := captureServiceBinary(uid)
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--serve</string><string>%d</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
<key>ProcessType</key><string>Background</string>
</dict></plist>
`, label, binary, uid))
}

func requireProtectedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("系统目录不安全：%s", path)
	}
	return nil
}

func installProtectedFile(path string, data io.Reader, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("安装目标不是普通文件：%s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".szjm-capture-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = io.Copy(temp, data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func InstallCaptureService(source string, uid int) error {
	if os.Geteuid() != 0 || uid <= 0 {
		return fmt.Errorf("安装流量接管服务需要管理员权限和有效用户")
	}
	for _, directory := range []string{"/Library/PrivilegedHelperTools", "/Library/LaunchDaemons"} {
		if err := requireProtectedDirectory(directory); err != nil {
			return err
		}
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", source).Run(); err != nil {
		return fmt.Errorf("辅助程序签名校验失败：%w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := installProtectedFile(captureServiceBinary(uid), input, 0o755); err != nil {
		return err
	}
	if err := installProtectedFile(captureServicePlist(uid), strings.NewReader(string(installCaptureServicePlist(uid))), 0o644); err != nil {
		return err
	}
	label := "system/" + captureServiceLabel(uid)
	if err := exec.Command("/bin/launchctl", "print", label).Run(); err == nil {
		if output, err := exec.Command("/bin/launchctl", "kickstart", "-k", label).CombinedOutput(); err != nil {
			return fmt.Errorf("启动流量接管服务失败：%w：%s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if output, err := exec.Command("/bin/launchctl", "bootstrap", "system", captureServicePlist(uid)).CombinedOutput(); err != nil {
		return fmt.Errorf("注册流量接管服务失败：%w：%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func UninstallCaptureService(uid int) error {
	if os.Geteuid() != 0 || uid <= 0 {
		return fmt.Errorf("移除流量接管服务需要管理员权限和有效用户")
	}
	label := "system/" + captureServiceLabel(uid)
	if err := exec.Command("/bin/launchctl", "print", label).Run(); err == nil {
		if output, err := exec.Command("/bin/launchctl", "bootout", label).CombinedOutput(); err != nil {
			return fmt.Errorf("停止流量接管服务失败：%w：%s", err, strings.TrimSpace(string(output)))
		}
	}
	for _, path := range []string{captureServicePlist(uid), captureServiceBinary(uid), CaptureServiceSocket(uid)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func CaptureServiceUID(value string) (int, error) {
	uid, err := strconv.Atoi(value)
	if err != nil || uid <= 0 {
		return 0, fmt.Errorf("无效的用户标识")
	}
	return uid, nil
}
