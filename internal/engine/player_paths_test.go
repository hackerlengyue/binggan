package engine

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func writePlayerFixture(t *testing.T, root, name string, qt bool) string {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("MZ\x00isolated-discovery-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if qt {
		for _, name := range []string{"Qt5Core.dll", "Qt5Network.dll"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func TestWindowsPlayerDiscoveryAcceptsPortableUnicodePaths(t *testing.T) {
	root := t.TempDir()
	path := writePlayerFixture(t, filepath.Join(root, "SZPlayer 26.06.551"), "MainPlayer.exe", true)
	var items []Installation
	for _, candidate := range playerDirectoryCandidates([]string{root}) {
		items = appendInstallation(items, candidate, "26.06.551", false)
	}
	if len(items) != 1 || items[0].Path != path {
		t.Fatalf("portable player not found: %+v", items)
	}
	items = appendInstallation(items, path, "", false)
	if len(items) != 1 {
		t.Fatal("duplicate candidate")
	}
	custom := writePlayerFixture(t, filepath.Join(root, "学习软件"), "MainPlayer.exe", true)
	if validWindowsPlayerPath(custom, false) {
		t.Fatal("generic MainPlayer accepted without vendor reference")
	}
	if !validWindowsPlayerPath(custom, true) {
		t.Fatal("explicit or vendor-registered portable path rejected")
	}
	for _, name := range []string{"pyxt.dll", "xldl.dll"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(custom), name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !validWindowsPlayerPath(custom, false) {
		t.Fatal("renamed vendor bundle rejected")
	}
}

func TestWindowsPlayerDiscoveryRejectsUnrelatedAndIncompleteExecutables(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		dir, name string
		qt        bool
	}{
		{"OtherPlayer", "MainPlayer.exe", true},
		{"NotSZPlayer", "MainPlayer.exe", true},
		{"SZPlayer 26.06.551", "UpDater.exe", true},
		{"SZPlayer missing dependencies", "MainPlayer.exe", false},
	} {
		path := writePlayerFixture(t, filepath.Join(root, tc.dir), tc.name, tc.qt)
		if validWindowsPlayerPath(path, false) {
			t.Fatal("unrelated executable accepted", tc.dir, tc.name)
		}
	}
	bad := writePlayerFixture(t, filepath.Join(root, "SZPlayer invalid"), "MainPlayer.exe", true)
	os.WriteFile(bad, []byte("not an executable"), 0600)
	if validWindowsPlayerPath(bad, true) {
		t.Fatal("non-PE file accepted")
	}
	if validWindowsPlayerPath(filepath.Join(root, "missing.exe"), true) {
		t.Fatal("missing executable accepted")
	}
}

func TestWindowsRegistryExecutableValuesAreParsedWithoutShell(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{`"C:\Program Files\SZPlayer\MainPlayer.exe" --open "%1"`, `C:\Program Files\SZPlayer\MainPlayer.exe`},
		{`C:\Program Files\SZPlayer\MainPlayer.exe,0`, `C:\Program Files\SZPlayer\MainPlayer.exe`},
		{`"D:\学习软件\MainPlayer.exe",0`, `D:\学习软件\MainPlayer.exe`},
		{`"incomplete`, ""},
		{``, ""},
		{`powershell -Command something`, ""},
	} {
		if got := executableFromRegistry(tc.value); got != tc.want {
			t.Fatalf("parse %q = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestWindowsPathPatternIsBoundedAndCaseInsensitive(t *testing.T) {
	r := regexp.MustCompile(WindowsPlayerPathPattern)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{`D:\课程\SZPlayer 26.06.551\MainPlayer.exe`, true},
		{`C:\Program Files\szplayer\MAINPLAYER.EXE`, true},
		{`D:\OtherPlayer\MainPlayer.exe`, false},
		{`D:\NotSZPlayer\MainPlayer.exe`, false},
		{`D:\SZPlayer\download\chatroom.exe`, false},
		{`D:\SZPlayer\MainPlayer.exe.extra`, false},
	} {
		if r.MatchString(tc.path) != tc.want {
			t.Fatal("incorrect player path match", tc.path)
		}
	}
}

func TestWindowsLocatedPlayerDoesNotInventChecksumOrProtocolVersion(t *testing.T) {
	err := &PlayerParametersUnavailable{Installation: Installation{Path: `D:\SZPlayer\MainPlayer.exe`, Version: "26.06.551.0"}}
	if !PlayerWasLocated(err) || !PlayerWasLocated(errors.Join(errors.New("context"), err)) {
		t.Fatal("located player not distinguished from missing player")
	}
	if PlayerWasLocated(errors.New("missing")) {
		t.Fatal("missing player treated as located")
	}
	if err.Error() == "" {
		t.Fatal("missing actionable diagnosis")
	}
}
