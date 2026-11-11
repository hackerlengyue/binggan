package engine

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsHashFixture(t *testing.T, content string) (Installation, string) {
	t.Helper()
	path := writePlayerFixture(t, filepath.Join(t.TempDir(), "SZPlayer 学习软件"), "MainPlayer.exe", true)
	data := []byte("MZ" + strings.Repeat(content, 131073))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	md5hex := fmt.Sprintf("%x", md5.Sum(data))
	return Installation{Path: path, Version: "test-only"}, md5hex
}

func TestWindowsPlayerAutomaticParametersReadActualExecutable(t *testing.T) {
	installation, want := windowsHashFixture(t, "actual-file-bytes")
	got, err := detectWindowsPlayer([]Installation{installation})
	if err != nil || got.SoftwareName != want || got.AppMD5 != want {
		t.Fatalf("automatic Windows parameters = %+v, %v; want both fields %s", got, err, want)
	}
}

func TestWindowsPlayerParametersVaryWithExecutableWithoutWhitelist(t *testing.T) {
	first, firstHash := windowsHashFixture(t, "custom-client-one")
	second, secondHash := windowsHashFixture(t, "custom-client-two")
	if firstHash == secondHash {
		t.Fatal("fixtures must contain different executables")
	}
	for _, tc := range []struct {
		installation Installation
		checksum     string
	}{{first, firstHash}, {second, secondHash}} {
		got, err := detectWindowsPlayer([]Installation{tc.installation})
		if err != nil || got.SoftwareName != tc.checksum || got.AppMD5 != tc.checksum {
			t.Errorf("custom client parameters = %+v, %v; want %s", got, err, tc.checksum)
		}
	}
}

func TestWindowsPlayerDetectionContinuesPastMissingInstallation(t *testing.T) {
	first, _ := windowsHashFixture(t, "missing")
	if err := os.Remove(first.Path); err != nil {
		t.Fatal(err)
	}
	second, want := windowsHashFixture(t, "readable")
	got, err := detectWindowsPlayer([]Installation{first, second})
	if err != nil || got.SoftwareName != want || got.AppMD5 != want {
		t.Fatalf("supported second installation was not detected: %+v, %v", got, err)
	}
}

func TestWindowsPlayerHashRecalculatesAfterReplacement(t *testing.T) {
	installation, original := windowsHashFixture(t, "original")
	first, err := detectWindowsPlayer([]Installation{installation})
	if err != nil || first.AppMD5 != original {
		t.Fatalf("original = %+v, %v", first, err)
	}
	replacement := []byte("MZreplacement")
	if err := os.WriteFile(installation.Path, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", md5.Sum(replacement))
	got, err := detectWindowsPlayer([]Installation{installation})
	if err != nil || got.AppMD5 != want || got.SoftwareName != want || got.AppMD5 == original {
		t.Fatalf("replacement reused previous parameters: %+v, %v; want %s", got, err, want)
	}
}

func TestWindowsPlayerHashRejectsInvalidAndMissingFiles(t *testing.T) {
	installation, _ := windowsHashFixture(t, "invalidated")
	if err := os.WriteFile(installation.Path, []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := detectWindowsPlayer([]Installation{installation}); err == nil || got != (Player{}) {
		t.Fatalf("invalid file returned parameters: %+v, %v", got, err)
	}
	if err := os.Remove(installation.Path); err != nil {
		t.Fatal(err)
	}
	if got, err := detectWindowsPlayer([]Installation{installation}); err == nil || got != (Player{}) {
		t.Fatalf("missing executable returned parameters: %+v, %v", got, err)
	}
	if _, err := detectWindowsPlayer(nil); err == nil || PlayerWasLocated(err) {
		t.Fatalf("missing installation was reported as located: %v", err)
	}
}

func TestWindowsPlayerHashDependsOnBytesRatherThanInstallDirectory(t *testing.T) {
	first, want := windowsHashFixture(t, "same-client")
	second, _ := windowsHashFixture(t, "same-client")
	for _, installation := range []Installation{first, second} {
		got, err := detectWindowsPlayer([]Installation{installation})
		if err != nil || got.AppMD5 != want || got.SoftwareName != want {
			t.Fatalf("same executable changed identity across directories: %+v, %v", got, err)
		}
	}
}
