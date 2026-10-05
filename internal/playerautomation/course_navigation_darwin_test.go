//go:build darwin && cgo

package playerautomation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeCourseNavigation(t *testing.T) {
	for _, source := range []string{"course_navigation_darwin.m", "testdata/course_navigation_test.m"} {
		if _, err := os.ReadFile(source); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(t.TempDir(), "course-navigation")
	command := exec.Command("clang", "-Wno-deprecated-declarations", "-framework", "Foundation", "-framework", "ApplicationServices", "testdata/course_navigation_test.m", "-o", binary)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile navigator fixture: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("navigator fixture: %v\n%s", err, output)
	}
}
