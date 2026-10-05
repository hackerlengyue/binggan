package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppBundleForExecutable(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "饼干大小姐.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := appBundleForExecutable(filepath.Join(bundle, "Contents", "MacOS", "饼干大小姐"))
	if err != nil || got != bundle {
		t.Fatalf("current bundle = %q, %v; want %q", got, err, bundle)
	}
	for _, path := range []string{
		filepath.Join(root, "not-an-app", "Contents", "MacOS", "饼干大小姐"),
		filepath.Join(bundle, "Contents", "Helpers", "permission-helper"),
		filepath.Join(root, "饼干大小姐.exe"),
	} {
		if _, err := appBundleForExecutable(path); err == nil {
			t.Fatalf("accepted a non-app executable: %q", path)
		}
	}
}
