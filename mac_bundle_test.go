//go:build darwin

package main

import (
	"debug/buildinfo"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BINGGAN_MAC_BUNDLE names the exact signed app being proposed for install.
// The default Go test path stays fast; the packaging script runs this gate.
func TestMacBundleIdentity(t *testing.T) {
	bundle := os.Getenv("BINGGAN_MAC_BUNDLE")
	if bundle == "" {
		t.Skip("set BINGGAN_MAC_BUNDLE to validate a packaged app")
	}
	infoPlist := filepath.Join(bundle, "Contents", "Info.plist")
	output, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", infoPlist).CombinedOutput()
	if err != nil {
		t.Fatalf("read packaged Bundle ID: %v: %s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "time.binggan.haomen.v2"; got != want {
		t.Fatalf("packaged Bundle ID=%q, want %q", got, want)
	}
	requirement, err := exec.Command("/usr/bin/codesign", "-dr", "-", bundle).CombinedOutput()
	if err != nil {
		t.Fatalf("read packaged code requirement: %v: %s", err, requirement)
	}
	if want := `designated => identifier "time.binggan.haomen.v2"`; !strings.Contains(string(requirement), want) {
		t.Fatalf("packaged code requirement=%q, want %q", strings.TrimSpace(string(requirement)), want)
	}
}

func TestMacBundleIncludesPlayerDriver(t *testing.T) {
	bundle := os.Getenv("BINGGAN_MAC_BUNDLE")
	if bundle == "" {
		t.Skip("set BINGGAN_MAC_BUNDLE to validate a packaged app")
	}
	binary := filepath.Join(bundle, "Contents", "MacOS", "饼干大小姐")
	if actual, err := appBundleForExecutable(binary); err != nil || actual != bundle {
		t.Fatalf("cannot reveal packaged app: got %q, %v; want %q", actual, err, bundle)
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if got := settings["CGO_ENABLED"]; got != "1" {
		t.Fatalf("Mac bundle omitted the AX player driver: CGO_ENABLED=%q, want 1", got)
	}
	if got := settings["GOOS"]; got != "darwin" {
		t.Fatalf("Mac bundle GOOS=%q, want darwin", got)
	}
	arch := os.Getenv("BINGGAN_MAC_ARCH")
	if arch != "" {
		arch = machoArchitecture(arch)
		out, err := exec.Command("/usr/bin/lipo", "-archs", binary).CombinedOutput()
		if err != nil {
			t.Fatalf("lipo -archs: %v: %s", err, out)
		}
		if !strings.Contains(" "+strings.TrimSpace(string(out))+" ", " "+arch+" ") {
			t.Fatalf("Mac bundle architectures %q omit %s", strings.TrimSpace(string(out)), arch)
		}
	}
	output, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput()
	if err != nil {
		t.Fatalf("Mac bundle signature: %v: %s", err, output)
	}
}

func TestMacBundleIncludesPermissionFlow(t *testing.T) {
	bundle := os.Getenv("BINGGAN_MAC_BUNDLE")
	if bundle == "" {
		t.Skip("set BINGGAN_MAC_BUNDLE to validate a packaged app")
	}
	resources := filepath.Join(bundle, "Contents", "Resources")
	helper := filepath.Join(resources, "tools", "binggan-permission-helper")
	info, err := os.Stat(helper)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		t.Fatalf("PermissionFlow helper is not executable: %v", err)
	}
	// SwiftPM and Xcode can produce different resource bundle layouts. Ask the
	// packaged helper to resolve translations through PermissionFlow itself.
	output, err := exec.Command(helper, "--verify-resources").CombinedOutput()
	if err != nil {
		t.Fatalf("PermissionFlow packaged resources: %v: %s", err, output)
	}
	var translations map[string]string
	if err := json.Unmarshal(output, &translations); err != nil {
		t.Fatalf("PermissionFlow resource verification: %v: %s", err, output)
	}
	for locale, want := range map[string]string{"zh-Hans": "辅助功能", "en": "Accessibility"} {
		if got := translations[locale]; got != want {
			t.Fatalf("PermissionFlow %s translation = %q, want %q", locale, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(resources, "licenses", "PermissionFlow-MIT.txt")); err != nil {
		t.Fatalf("PermissionFlow license missing: %v", err)
	}
	if arch := os.Getenv("BINGGAN_MAC_ARCH"); arch != "" {
		arch = machoArchitecture(arch)
		out, err := exec.Command("/usr/bin/lipo", "-archs", helper).CombinedOutput()
		if err != nil || !strings.Contains(" "+strings.TrimSpace(string(out))+" ", " "+arch+" ") {
			t.Fatalf("PermissionFlow helper architectures %q omit %s: %v", strings.TrimSpace(string(out)), arch, err)
		}
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", helper).CombinedOutput(); err != nil {
		t.Fatalf("PermissionFlow helper signature: %v: %s", err, out)
	}
	if err := exec.Command(helper).Run(); err == nil {
		t.Fatal("PermissionFlow helper accepted missing app path")
	} else if failure, ok := err.(*exec.ExitError); !ok || failure.ExitCode() != 2 {
		t.Fatalf("PermissionFlow helper argument validation: %v", err)
	}
}

func machoArchitecture(goarch string) string {
	if goarch == "amd64" {
		return "x86_64"
	}
	return goarch
}
