package engine

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsExplicitPortableLocation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "学习软件")
	path := writePlayerFixture(t, root, "MainPlayer.exe", true)
	t.Setenv("SZJM_PLAYER_PATH", path)
	items := WindowsPlayerInstallations()
	if len(items) == 0 || items[0].Path != path {
		t.Fatalf("explicit portable path not found: %+v", items)
	}
	_, err := DetectPlayer()
	if !PlayerWasLocated(err) {
		t.Fatalf("located player reported as missing: %v", err)
	}
}

func TestWindowsRunningPlayerLocation(t *testing.T) {
	if os.Getenv("SZJM_PLAYER_TEST_CHILD") == "1" {
		os.Stdout.WriteString("ready\n")
		io.Copy(io.Discard, os.Stdin)
		return
	}
	t.Setenv("SZJM_PLAYER_PATH", "")
	root := filepath.Join(t.TempDir(), "SZPlayer test-only process")
	path := writePlayerFixture(t, root, "MainPlayer.exe", true)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Create(path)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, err = io.Copy(target, source)
	source.Close()
	target.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "-test.run=^TestWindowsRunningPlayerLocation$")
	cmd.Env = append(os.Environ(), "SZJM_PLAYER_TEST_CHILD=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("test-only process not ready: %q %v", line, err)
	}
	for _, item := range WindowsPlayerInstallations() {
		if item.Path == path {
			return
		}
	}
	t.Fatal("native process snapshot did not locate the test-only player")
}
