// Package surge lends only Shenzao TCP traffic to the local capture receiver.
// Surge remains the sole owner of the system proxy and virtual interface.
package surge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"time.haomen/binggan/v2/internal/capture"
)

type runner func(context.Context, ...string) ([]byte, error)

type Manager struct {
	run       runner
	home, dir string
	journal   *journal
}

type journal struct {
	Profile string `json:"profile"`
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Block   string `json:"block"`
}

func cliPath() string {
	const suffix = "Surge.app/Contents/Applications/surge-cli"
	home, _ := os.UserHomeDir()
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
		path := filepath.Join(root, suffix)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func commandRunner(path string) runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, path, args...).Output()
		if err != nil {
			// CLI diagnostics can contain profile credentials. Never propagate them.
			return nil, fmt.Errorf("Surge 命令未成功执行（%s），请检查 Surge 是否运行及配置是否有效", args[0])
		}
		return out, nil
	}
}

// Active is read-only. A failed controller check must not silently start a
// competing TUN while Surge is running.
func Active(ctx context.Context) (bool, error) {
	if runtime.GOOS != "darwin" {
		return false, nil
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := exec.CommandContext(probe, "/usr/bin/pgrep", "-x", "Surge").Run(); err != nil {
		if probe.Err() != nil {
			return false, fmt.Errorf("检查 Surge 运行状态超时")
		}
		return false, nil
	}
	path := cliPath()
	if path == "" {
		return false, fmt.Errorf("Surge 正在运行，但未找到 surge-cli，无法安全建立共存连接")
	}
	run := commandRunner(path)
	for _, name := range []string{"system-proxy", "enhanced-mode"} {
		out, err := run(ctx, "--raw", "feature", "get", name)
		if err != nil {
			return false, err
		}
		var result struct {
			Enabled *bool `json:"enabled"`
		}
		if json.Unmarshal(out, &result) != nil || result.Enabled == nil {
			return false, fmt.Errorf("Surge 版本不支持自动共存状态检查")
		}
		if *result.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func New(dir string) (*Manager, error) {
	path := cliPath()
	if path == "" {
		return nil, fmt.Errorf("未找到 Surge 命令行工具")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Manager{run: commandRunner(path), home: home, dir: dir}, nil
}

func (m *Manager) current(ctx context.Context) (string, error) {
	out, err := m.run(ctx, "--raw", "profile", "current")
	if err != nil {
		return "", err
	}
	var result struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(out, &result) != nil || result.Profile == "" || strings.ContainsAny(result.Profile, "/\\\r\n") {
		return "", fmt.Errorf("无法确认当前 Surge 配置")
	}
	return result.Profile, nil
}

func (m *Manager) profilePath(name string) (string, error) {
	for _, dir := range []string{
		filepath.Join(m.home, "Library", "Application Support", "Surge", "Profiles"),
		filepath.Join(m.home, "Library", "Mobile Documents", "iCloud~com~nssurge~inc~Surge", "Documents"),
	} {
		path := filepath.Join(dir, name+".conf")
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到当前 Surge 本地配置文件；请将配置保存在 Surge 默认 Profiles 目录后重试")
}

func (m *Manager) ruleMode(ctx context.Context) error {
	out, err := m.run(ctx, "--raw", "mode")
	if err != nil {
		return err
	}
	var result struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(out, &result) != nil || result.Mode != "rule" {
		return fmt.Errorf("Surge 需要处于规则模式才能仅转交深造流量；不会自动改变全局出站模式")
	}
	return nil
}

func (m *Manager) Enable(ctx context.Context, d capture.Descriptor) (err error) {
	if m.journal != nil {
		return m.Check(ctx)
	}
	if err = m.ruleMode(ctx); err != nil {
		return err
	}
	profile, err := m.current(ctx)
	if err != nil {
		return err
	}
	path, err := m.profilePath(profile)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	patched, block, err := patchProfile(string(original), d)
	if err != nil {
		return err
	}
	rule, err := captureRule(d.BackendExecutable)
	if err != nil {
		return err
	}
	if err = m.validate(ctx, path, patched); err != nil {
		return err
	}
	// Journal before mutation, so interruption between any two steps can be
	// recovered without replacing unrelated edits made by the user.
	j := &journal{Profile: profile, Path: path, Rule: rule, Block: block}
	data, _ := json.Marshal(j)
	if err = writeAtomic(filepath.Join(m.dir, "session.json"), data, 0600); err != nil {
		return err
	}
	m.journal = j
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if e := m.Disable(cleanup); e != nil {
				err = errors.Join(err, fmt.Errorf("恢复 Surge 配置失败：%w", e))
			}
		}
	}()
	if err = replaceUnchanged(path, original, []byte(patched)); err != nil {
		return err
	}
	if _, err = m.run(ctx, "reload"); err != nil {
		return err
	}
	if _, err = m.run(ctx, "rule", "temp", "add", rule); err != nil {
		return err
	}
	return m.Check(ctx)
}

func (m *Manager) validate(ctx context.Context, path, content string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".binggan-check-*.conf")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.WriteString(content)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	_, err = m.run(ctx, "--check", name)
	return err
}

func (m *Manager) Check(ctx context.Context) error {
	if m.journal == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := m.ruleMode(ctx); err != nil {
		return err
	}
	profile, err := m.current(ctx)
	if err != nil {
		return err
	}
	if profile != m.journal.Profile {
		return fmt.Errorf("Surge 当前配置已切换，深造抓包连接需要重新建立")
	}
	rules, err := m.temporaryRules(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if sameRule(rule, m.journal.Rule) {
			// A managed-profile refresh can remove the policy while leaving the
			// temporary rule in memory. Confirm effective routing, not just presence.
			out, err := m.run(ctx, "--raw", "rule", "match", "www.shenzaokeji.com", "protocol=HTTPS", "process-path=/__binggan_route_probe__/SzPlayer")
			if err != nil {
				return err
			}
			var match struct {
				Policy string `json:"policy"`
			}
			if json.Unmarshal(out, &match) != nil || match.Policy != policyName {
				return fmt.Errorf("Surge 深造转发策略未生效，请重新连接")
			}
			return nil
		}
	}
	return fmt.Errorf("Surge 深造抓包临时规则已失效，请重新连接")
}

func (m *Manager) temporaryRules(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "--raw", "rule", "temp", "list")
	if err != nil {
		return nil, err
	}
	var result struct {
		Rules []string `json:"rules"`
	}
	if json.Unmarshal(out, &result) != nil {
		return nil, fmt.Errorf("无法读取 Surge 临时规则")
	}
	return result.Rules, nil
}

// Recover is called while holding the helper's exclusive process lock.
func (m *Manager) Recover(ctx context.Context) error {
	data, err := os.ReadFile(filepath.Join(m.dir, "session.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var j journal
	if json.Unmarshal(data, &j) != nil || j.Rule == "" || j.Block == "" || !filepath.IsAbs(j.Path) {
		return fmt.Errorf("Surge 恢复记录无效，已保留现场")
	}
	m.journal = &j
	return m.Disable(ctx)
}

func (m *Manager) Disable(ctx context.Context) error {
	j := m.journal
	if j == nil {
		return nil
	}
	// Remove only our exact temporary rule, never flush the user's rules.
	rules, removeErr := m.temporaryRules(ctx)
	if removeErr == nil {
		for _, rule := range rules {
			if sameRule(rule, j.Rule) {
				_, removeErr = m.run(ctx, "rule", "temp", "remove", rule)
				break
			}
		}
	}
	if removeErr != nil && surgeProcessRunning() {
		return removeErr
	}
	if removeErr == nil {
		remaining, err := m.temporaryRules(ctx)
		if err != nil {
			return err
		}
		for _, rule := range remaining {
			if sameRule(rule, j.Rule) {
				return fmt.Errorf("Surge 尚未确认移除深造分流，已保留策略以避免连接中断")
			}
		}
	}
	current, err := os.ReadFile(j.Path)
	if err != nil {
		return err
	}
	if strings.Contains(string(current), markerStart) {
		restored, err := removeBlock(string(current), j.Block)
		if err != nil {
			return err
		}
		if err = replaceUnchanged(j.Path, current, []byte(restored)); err != nil {
			return err
		}
	}
	active, activeErr := m.current(ctx)
	if activeErr == nil && active == j.Profile {
		if _, err = m.run(ctx, "reload"); err != nil {
			return err
		}
		if removeErr != nil {
			return removeErr
		}
	}
	// If Surge has quit, temporary rules have gone with it. The disk profile
	// above is already restored before its next launch.
	if activeErr != nil && surgeProcessRunning() {
		return activeErr
	}
	if err = os.Remove(filepath.Join(m.dir, "session.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.journal = nil
	return nil
}

func surgeProcessRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/pgrep", "-x", "Surge").Run() == nil
}

func replaceUnchanged(path string, before, after []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(current) != string(before) {
		return fmt.Errorf("Surge 配置正在被其他程序修改，请稍后重试")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return writeAtomic(path, after, info.Mode().Perm())
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".binggan-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
