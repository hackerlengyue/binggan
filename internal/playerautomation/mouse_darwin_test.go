//go:build darwin && cgo

package playerautomation

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMouseEventsCarryWindowIdentity(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mouse-events")
	build := exec.Command("xcrun", "clang", "testdata/mouse_events.m", "-framework", "AppKit", "-framework", "ApplicationServices", "-o", binary)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native event fixture: %v\n%s", err, output)
	}
	// The previous bare CGEvent implementation must fail this same check.
	if output, err := exec.Command(binary, "--legacy").CombinedOutput(); err == nil || !strings.Contains(string(output), "missing AppKit window identity") {
		t.Fatalf("regression fixture did not reject the old event: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("native event validation failed: %v\n%s", err, output)
	}
}

func TestFullscreenProgressCanLocateWindowOnAnotherSpace(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fullscreen-progress")
	build := exec.Command("xcrun", "clang", "testdata/fullscreen_progress.m", "-framework", "AppKit", "-framework", "ApplicationServices", "-o", binary)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fullscreen fixture: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("fullscreen progress validation failed: %v\n%s", err, output)
	}
}
