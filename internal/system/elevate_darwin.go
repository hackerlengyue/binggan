package system

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func LaunchHelper(parent context.Context, executable, address, token string) error {
	uid := os.Geteuid()
	if uid <= 0 {
		return fmt.Errorf("请从当前登录用户的桌面打开软件")
	}
	found, err := requestCaptureService(parent, uid, address, token)
	if found && !errors.Is(err, ErrCaptureServiceUpgrade) {
		return err
	}
	source := bundledCaptureService(executable)
	if _, statErr := os.Stat(source); errors.Is(statErr, os.ErrNotExist) && !strings.Contains(filepath.ToSlash(executable), ".app/Contents/MacOS/") {
		// During a MyGo development run the executable lives in a temporary
		// directory, while resources retain their per-platform source layout.
		if workDir, err := os.Getwd(); err == nil {
			devSource := filepath.Join(workDir, "resources", runtime.GOOS+"-"+runtime.GOARCH, "tools", "capture-service")
			if info, err := os.Stat(devSource); err == nil && info.Mode().IsRegular() {
				source = devSource
			}
		}
	}
	if info, statErr := os.Stat(source); statErr != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("安装包缺少流量接管服务，请重新安装饼干大小姐")
	}
	if err := exec.CommandContext(parent, "/usr/bin/codesign", "--verify", "--strict", source).Run(); err != nil {
		return fmt.Errorf("流量接管服务签名校验失败：%w", err)
	}
	script := quote(source) + " --install " + strconv.Itoa(uid)
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	out, e := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "do shell script "+strconv.Quote(script)+" with administrator privileges").CombinedOutput()
	if e != nil {
		return commandError(ctx, "系统授权未完成", out, e)
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	ready := time.NewTimer(15 * time.Second)
	defer ready.Stop()
	for {
		found, err = requestCaptureService(ctx, uid, address, token)
		if found && err == nil {
			return nil
		}
		if found && !errors.Is(err, ErrCaptureServiceUpgrade) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready.C:
			return fmt.Errorf("流量接管服务已安装，但启动超时，请稍后重试")
		case <-ticker.C:
		}
	}
}

// Keychain changes run in the logged-in user session, not the elevated TUN helper.
func InstallCertificate(parent context.Context, path string, progress CertificateProgress) error {
	return importCertificate(parent, path, os.Geteuid(), securityExec, progress)
}

func TrustCertificate(parent context.Context, path string, progress CertificateProgress) error {
	return trustCertificate(parent, path, os.Geteuid(), securityExec, progress)
}

func CertificateInstalled(parent context.Context, path string) (bool, error) {
	return certificateInstalled(parent, path, securityExec)
}

type securityCommand func(context.Context, ...string) ([]byte, error)

func securityExec(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "/usr/bin/security", args...).CombinedOutput()
}

func importCertificate(parent context.Context, path string, uid int, run securityCommand, progress CertificateProgress) error {
	if e := requireUserSession(uid); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	cn, fingerprint, e := certificateIdentity(path)
	if e != nil {
		return e
	}
	progress.Report("正在准备证书安装", "读取当前用户的默认钥匙串")
	keychain, out, e := userKeychain(ctx, run)
	if e != nil {
		return commandError(ctx, "无法读取登录钥匙串", out, e)
	}
	present, e := certificateInKeychain(ctx, run, keychain, cn, fingerprint)
	if e != nil {
		return e
	}
	if present {
		progress.Report("证书已在系统中", "登录钥匙串里已有当前证书，尚未因此设置为信任")
		return nil
	}
	progress.Report("正在写入登录钥匙串", "这一步只安装证书，不会弹出信任授权")
	out, e = run(ctx, "add-certificates", "-k", keychain, path)
	if e != nil && !alreadyInKeychain(out) {
		return commandError(ctx, "证书安装未完成", out, e)
	}
	present, e = certificateInKeychain(ctx, run, keychain, cn, fingerprint)
	if e != nil {
		return e
	}
	if !present {
		return fmt.Errorf("证书安装未完成：证书没有出现在登录钥匙串")
	}
	progress.Report("证书已安装到系统", "请继续信任证书")
	return nil
}

func trustCertificate(parent context.Context, path string, uid int, run securityCommand, progress CertificateProgress) error {
	if e := requireUserSession(uid); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	progress.Report("正在检查已有信任", "读取 macOS 当前用户的证书信任设置")
	verify := []string{"verify-cert", "-c", path, "-p", "basic", "-L"}
	if _, e := run(ctx, verify...); e == nil {
		progress.Report("证书已被系统信任", "无需再次授权")
		return nil
	}
	if ctx.Err() != nil {
		return commandError(ctx, "证书信任未完成", nil, ctx.Err())
	}
	cn, fingerprint, e := certificateIdentity(path)
	if e != nil {
		return e
	}
	keychain, out, e := userKeychain(ctx, run)
	if e != nil {
		return commandError(ctx, "无法读取登录钥匙串", out, e)
	}
	present, e := certificateInKeychain(ctx, run, keychain, cn, fingerprint)
	if e != nil {
		return e
	}
	if !present {
		return fmt.Errorf("请先将证书安装到系统，再信任证书")
	}
	progress.Report("等待系统授权", "请在 macOS 系统弹窗中确认信任")
	out, e = run(ctx, "add-trusted-cert", "-r", "trustRoot", "-k", keychain, path)
	if e != nil {
		return commandError(ctx, "证书信任未完成", out, e)
	}
	progress.Report("正在校验系统信任", "确认当前用户可以使用该 CA 证书")
	if out, e = run(ctx, verify...); e != nil {
		return commandError(ctx, "证书尚未被系统信任", out, e)
	}
	return nil
}

func certificateInstalled(parent context.Context, path string, run securityCommand) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	cn, fingerprint, e := certificateIdentity(path)
	if e != nil {
		return false, e
	}
	keychain, out, e := userKeychain(ctx, run)
	if e != nil {
		return false, commandError(ctx, "无法读取登录钥匙串", out, e)
	}
	return certificateInKeychain(ctx, run, keychain, cn, fingerprint)
}

func requireUserSession(uid int) error {
	if uid == 0 {
		return fmt.Errorf("证书信任需要登录用户会话，请正常打开饼干大小姐，不要以 sudo 或后台服务启动")
	}
	return nil
}

func userKeychain(ctx context.Context, run securityCommand) (string, []byte, error) {
	out, e := run(ctx, "default-keychain", "-d", "user")
	if e != nil {
		return "", out, e
	}
	keychain, e := strconv.Unquote(strings.TrimSpace(string(out)))
	if e != nil || keychain == "" {
		return "", out, fmt.Errorf("无法读取登录钥匙串路径，请先登录 macOS 并解锁钥匙串")
	}
	return keychain, out, nil
}

func certificateInKeychain(ctx context.Context, run securityCommand, keychain, cn, fingerprint string) (bool, error) {
	out, err := run(ctx, "find-certificate", "-a", "-Z", "-c", cn, keychain)
	if err != nil {
		if ctx.Err() != nil {
			return false, commandError(ctx, "无法检查登录钥匙串中的证书", out, err)
		}
		text := strings.ToLower(string(out))
		if strings.Contains(text, "could not be found") || strings.Contains(text, "not found") || strings.Contains(text, "-25300") || strings.Contains(text, "找不到") {
			return false, nil
		}
		return false, commandError(ctx, "无法检查登录钥匙串中的证书", out, err)
	}
	return outputHasFingerprint(string(out), fingerprint), nil
}

func certificateIdentity(path string) (string, string, error) {
	raw, e := os.ReadFile(path)
	if e != nil {
		return "", "", e
	}
	der := raw
	if block, _ := pem.Decode(raw); block != nil {
		der = block.Bytes
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		return "", "", fmt.Errorf("无法读取证书：%w", e)
	}
	sum := sha1.Sum(cert.Raw)
	return cert.Subject.CommonName, hex.EncodeToString(sum[:]), nil
}

func alreadyInKeychain(out []byte) bool {
	text := strings.ToLower(string(out))
	return strings.Contains(text, "already exists") || strings.Contains(text, "-25299")
}

func commandError(ctx context.Context, action string, out []byte, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("%s：操作已取消", action)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s：等待系统授权超时，请打开“证书管理”后重试", action)
	}
	detail := strings.TrimSpace(string(out))
	if strings.Contains(detail, "no user interaction was possible") {
		return fmt.Errorf("%s：macOS 无法显示授权窗口。请从已登录的桌面打开软件，并在“证书管理”中重新安装（%s）", action, detail)
	}
	if strings.Contains(detail, "User canceled") || strings.Contains(detail, "User cancelled") || strings.Contains(detail, "(-128)") {
		return fmt.Errorf("%s：已取消系统授权，可在“证书管理”中重试", action)
	}
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s：%s", action, detail)
}
