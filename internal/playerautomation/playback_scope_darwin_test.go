//go:build darwin && cgo

package playerautomation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePlaybackReadsIgnoreCourseTree(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	extract := func(start, end string) string {
		a, b := strings.Index(text, start), strings.Index(text, end)
		if a < 0 || b <= a {
			t.Fatalf("native reader missing: %s", start)
		}
		return text[a:b]
	}
	readers := extract("static int copyString(", "static int containsText(")
	controls := extract("static AXUIElementRef findPlaybackButton(", "static int togglePlayerPlayback(")
	// The old signature is supported so the same fixture can run red against an
	// isolated checkout. It still executes the production C implementation.
	call := "axSnapshot(1, &incomplete)"
	fullCall := call
	if strings.Contains(readers, "int playbackOnly") {
		call = "axSnapshot(1, &incomplete, 1)"
		fullCall = "axSnapshot(1, &incomplete, 0)"
	}
	fixture, err := os.ReadFile("testdata/playback_scope_test.c")
	if err != nil {
		t.Fatal(err)
	}
	code := strings.ReplaceAll(string(fixture), "/* PRODUCTION_READERS */", readers)
	code = strings.ReplaceAll(code, "/* PRODUCTION_CONTROLS */", controls)
	code = strings.ReplaceAll(code, "PLAYBACK_SNAPSHOT", call)
	code = strings.ReplaceAll(code, "FULL_SNAPSHOT", fullCall)
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "scope.c"), filepath.Join(dir, "scope")
	if err := os.WriteFile(file, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("playback scope: %v\n%s", err, out)
	}
}
