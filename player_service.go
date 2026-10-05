package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/playerautomation"
)

// PlayerService contains only the visible, bounded SzPlayer operations.
type PlayerService struct{ app *Workbench }

// Wait keeps orchestration timing in the native process when WKWebView is hidden.
func (s *PlayerService) Wait(ctx context.Context, milliseconds int) error {
	return playerautomation.Wait(ctx, milliseconds)
}

func (s *PlayerService) Status() playerautomation.Status              { return s.app.player.Status() }
func (s *PlayerService) Snapshot() (playerautomation.Snapshot, error) { return s.app.player.Snapshot() }
func (s *PlayerService) PlaybackSnapshot() (playerautomation.Snapshot, error) {
	return s.app.player.PlaybackSnapshot()
}
func (s *PlayerService) CourseSnapshot() (playerautomation.Snapshot, error) {
	return s.app.player.CourseSnapshot()
}
func (s *PlayerService) RevealCourse(request playerautomation.CourseTargetRequest) (playerautomation.Snapshot, error) {
	return s.app.player.RevealCourse(request)
}
func (s *PlayerService) ExpandFolder(name string) error   { return s.app.player.ExpandFolder(name) }
func (s *PlayerService) OpenCourse(path string) error     { return s.app.player.OpenCourse(path) }
func (s *PlayerService) SetVolume(percent int) error      { return s.app.player.SetVolume(percent) }
func (s *PlayerService) SetFullscreen(enabled bool) error { return s.app.player.SetFullscreen(enabled) }
func (s *PlayerService) SetPlaybackRate(rate float64) error {
	return s.app.player.SetPlaybackRate(rate)
}
func (s *PlayerService) TogglePlayback() error { return s.app.player.TogglePlayback() }
func (s *PlayerService) Click(request playerautomation.ClickRequest) error {
	return s.app.player.Click(request)
}

// OpenPlayer launches the installed SzPlayer by its bundle identifier, so it works
// wherever the app is installed. Orchestration itself only exists on macOS.
func (s *PlayerService) OpenPlayer() error {
	if runtime.GOOS != "darwin" {
		return playerautomation.ErrUnsupported
	}
	if err := exec.Command("open", "-b", playerautomation.BundleID).Run(); err != nil {
		return errors.New("无法打开 SzPlayer，请确认它已安装")
	}
	return nil
}

func (s *PlayerService) OpenPermissionSettings() error {
	if err := s.app.player.RequestAccessibilityPermission(); err != nil {
		return err
	}
	return launchPermissionFlow(s.app)
}

func (s *PlayerService) RevealAppForPermission() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	bundle, err := appBundleForExecutable(executable)
	if err != nil {
		return err
	}
	mygo.Shell.ShowItemInFolder(bundle)
	return nil
}

func appBundleForExecutable(executable string) (string, error) {
	macOSDir := filepath.Dir(filepath.Clean(executable))
	contentsDir := filepath.Dir(macOSDir)
	bundle := filepath.Dir(contentsDir)
	if filepath.Base(macOSDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || filepath.Ext(bundle) != ".app" {
		return "", errors.New("请从已安装的饼干大小姐应用中打开权限设置")
	}
	info, err := os.Stat(bundle)
	if err != nil || !info.IsDir() {
		return "", errors.New("无法定位当前应用")
	}
	return bundle, nil
}
