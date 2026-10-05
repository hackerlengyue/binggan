package engine

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlayerDigestCacheInvalidatesExecutableChanges(t *testing.T) {
	app := t.TempDir()
	dir := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	plist := []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>Player</string><key>CFBundleShortVersionString</key><string>test.1</string></dict></plist>`)
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), plist, 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "Player")
	contents := []byte("original executable")
	if err := os.WriteFile(exe, contents, 0600); err != nil {
		t.Fatal(err)
	}
	first, err := PlayerAt(app)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(contents)
	digest := md5.Sum([]byte(hex.EncodeToString(sha[:])))
	if first.AppMD5 != hex.EncodeToString(digest[:]) {
		t.Fatal("digest changed")
	}
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := playerFileHash(f)
	position, _ := f.Seek(0, 1)
	f.Close()
	if err != nil || got != first.AppMD5 || position != 0 {
		t.Fatal("warm cache reread executable")
	}
	stamp := time.Now().Add(time.Second)
	if err := os.WriteFile(exe, []byte("modified executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(exe, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	second, err := PlayerAt(app)
	if err != nil || second.AppMD5 == first.AppMD5 {
		t.Fatal("same inode update not detected")
	}
	info, _ := os.Stat(exe)
	replacement := exe + ".new"
	if err := os.WriteFile(replacement, contents, 0600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(replacement, info.ModTime(), info.ModTime())
	os.Remove(exe)
	if err := os.Rename(replacement, exe); err != nil {
		t.Fatal(err)
	}
	third, err := PlayerAt(app)
	if err != nil || third.AppMD5 != first.AppMD5 {
		t.Fatal("same size/time replacement not detected")
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := PlayerAt(app); err == nil {
		t.Fatal("missing executable served from cache")
	}
}
