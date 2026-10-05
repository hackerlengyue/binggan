//go:build darwin

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

const accessibilitySettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

func launchPermissionFlow(app *Workbench) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	bundle, err := appBundleForExecutable(executable)
	if err != nil {
		if mygo.IsDev() {
			return mygo.Shell.OpenExternal(accessibilitySettingsURL)
		}
		return err
	}
	resourceDir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return err
	}
	helper := filepath.Join(resourceDir, "tools", "binggan-permission-helper")
	if info, err := os.Stat(helper); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		if mygo.IsDev() {
			return mygo.Shell.OpenExternal(accessibilitySettingsURL)
		}
		return fmt.Errorf("权限引导程序缺失：%s", helper)
	}
	app.permissionMu.Lock()
	defer app.permissionMu.Unlock()
	if previous := app.permissionCmd; previous != nil {
		// A second click replaces the prior panel instead of stacking helpers.
		app.permissionCmd = nil
		_ = previous.Process.Kill()
	}
	cmd := exec.CommandContext(app.ctx, helper, "--app", bundle)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动权限引导失败：%w", err)
	}
	app.permissionCmd = cmd
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(done)
		app.permissionMu.Lock()
		current := app.permissionCmd == cmd
		if current {
			app.permissionCmd = nil
		}
		app.permissionMu.Unlock()
		if current && err != nil && app.ctx.Err() == nil {
			slog.Warn("权限引导程序退出", "error", err)
		}
	}()
	go watchPermissionFlowGrant(app, cmd, done)
	return nil
}

// PermissionFlow runs in a separate process, so its AX trust check observes the
// helper rather than this app. Dismiss its panel when the app itself is trusted.
func watchPermissionFlowGrant(app *Workbench, cmd *exec.Cmd, done <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-app.ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if !app.player.Status().AccessibilityReady {
				continue
			}
			app.permissionMu.Lock()
			if app.permissionCmd == cmd {
				app.permissionCmd = nil
				_ = cmd.Process.Kill()
			}
			app.permissionMu.Unlock()
			return
		}
	}
}
