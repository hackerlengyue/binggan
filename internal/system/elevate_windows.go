package system

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func LaunchHelper(_ context.Context, executable, address, token string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	exe, _ := windows.UTF16PtrFromString(executable)
	args, _ := windows.UTF16PtrFromString(syscall.EscapeArg("--capture-helper") + " " + syscall.EscapeArg(address) + " " + syscall.EscapeArg(token))
	return windows.ShellExecute(0, verb, exe, args, nil, windows.SW_HIDE)
}
func InstallCertificate(parent context.Context, path string, progress CertificateProgress) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	progress.Report("正在安装到系统", "导入当前用户的中间证书存储，这一步不设置信任")
	if e := certutil(ctx, "-user", "-f", "-addstore", "CA", path); e != nil {
		return e
	}
	progress.Report("证书已安装到系统", "请继续信任证书")
	return nil
}

func TrustCertificate(parent context.Context, path string, progress CertificateProgress) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	trusted, e := CertificateTrustedContext(ctx, string(raw))
	if e != nil {
		return e
	}
	if trusted {
		progress.Report("证书已被系统信任", "无需再次授权")
		return nil
	}
	installed, e := certificateInStore(ctx, "CA", raw)
	if e != nil {
		return e
	}
	if !installed {
		return fmt.Errorf("请先将证书安装到系统，再信任证书")
	}
	progress.Report("等待系统授权", "导入当前用户的受信任根证书存储；如有系统弹窗，请完成授权")
	if e = certutil(ctx, "-user", "-f", "-addstore", "Root", path); e != nil {
		return e
	}
	progress.Report("正在校验系统信任", "确认当前用户可以使用该 CA 证书")
	return nil
}

func CertificateInstalled(parent context.Context, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	raw, e := os.ReadFile(path)
	if e != nil {
		return false, e
	}
	return certificateInStore(ctx, "CA", raw)
}

func certutil(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "certutil.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if e == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("证书操作已取消或超时，请在“证书管理”中重试")
	}
	return fmt.Errorf("证书操作失败：%s", strings.TrimSpace(string(out)))
}

func certificateInStore(ctx context.Context, store string, raw []byte) (bool, error) {
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		return false, fmt.Errorf("无法读取证书：%w", e)
	}
	sum := sha1.Sum(cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	cmd := exec.CommandContext(ctx, "certutil.exe", "-user", "-store", store)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return false, fmt.Errorf("证书操作已取消或超时，请在“证书管理”中重试")
	}
	if e != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = e.Error()
		}
		return false, fmt.Errorf("无法读取 Windows 证书存储：%s", detail)
	}
	return outputHasFingerprint(string(out), fingerprint), nil
}
