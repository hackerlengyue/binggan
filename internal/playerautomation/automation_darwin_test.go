//go:build darwin && cgo

package playerautomation

import (
	"os"
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
	if status.Version != verifiedPlayerVersion || !status.VerifiedProfile {
		t.Fatalf("installed SzPlayer version mismatch: %+v", status)
	}
}
