//go:build darwin && cgo

package playerautomation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestParseSnapshotPreservesLoginPlaceholder(t *testing.T) {
	snapshot, err := parseSnapshot("AXTextField\t\tsaved-user\t\t0\t2\t0\t0\t0\t输入用户名\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Elements) != 1 || snapshot.Elements[0].Placeholder != "输入用户名" {
		t.Fatalf("login placeholder lost: %+v", snapshot.Elements)
	}
}

func TestInstalledSzPlayerStatus(t *testing.T) {
	if os.Getenv("BINGGAN_TEST_INSTALLED_SZPLAYER") != "1" {
		t.Skip("set BINGGAN_TEST_INSTALLED_SZPLAYER=1 on a Mac with SzPlayer installed")
	}
	status := New().Status()
	if !status.Installed {
		t.Fatalf("installed SzPlayer was not detected: %+v", status)
	}
	if _, verified := profileForVersion(status.Version); !verified || !status.VerifiedProfile {
		t.Fatalf("installed SzPlayer version mismatch: %+v", status)
	}
	if err := requireVerifiedPlayer(); err != nil {
		t.Fatalf("installed player is blocked by action guard: %v", err)
	}
	t.Logf("version=%s verified=%t running=%t", status.Version, status.VerifiedProfile, status.Running)
}

func TestInstalledSzPlayer261056Controls(t *testing.T) {
	if os.Getenv("BINGGAN_TEST_INSTALLED_SZPLAYER") != "1" || playerVersion(playerPath()) != "26.10.56" {
		t.Skip("requires installed SzPlayer 26.10.56 and BINGGAN_TEST_INSTALLED_SZPLAYER=1")
	}
	binary := filepath.Join(t.TempDir(), "controls")
	if out, err := exec.Command("xcrun", "clang", "-fobjc-arc", "testdata/installed_controls.m", "-framework", "AppKit", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile nib probe: %v\n%s", err, out)
	}
	profile, _ := profileForVersion("26.10.56")
	format := func(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
	out, err := exec.Command(binary, filepath.Join(playerPath(), "Contents", "Resources", "PlayerControlBar.nib"),
		format(profile.playbackRelativeX), format(profile.playbackOffsetX), format(profile.playbackOffsetY)).CombinedOutput()
	if err != nil {
		t.Fatalf("installed nib contract: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
