package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAudioEncoderSelection(t *testing.T) {
	if !hasEncoder(" A..... aac_at  AAC AudioToolbox\n", "aac_at") || hasEncoder(" A..... aac  AAC\n", "aac_at") {
		t.Fatal("incorrect encoder availability")
	}
	t.Setenv("SZJM_AAC_ENCODER", "aac")
	name, fallback, err := audioEncoder(context.Background())
	if err != nil || name != "aac" || fallback {
		t.Fatalf("explicit compatibility encoder: %q %v %v", name, fallback, err)
	}
	t.Setenv("SZJM_AAC_ENCODER", "unknown")
	if _, _, err = audioEncoder(context.Background()); err == nil {
		t.Fatal("unknown encoder preference accepted")
	}
}

func TestPlatformEncoderFailureFallsBack(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AudioToolbox auto-selection is macOS only")
	}
	toolsAvailable(t)
	realFFmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wrapper := `#!/bin/sh
previous=""
for argument in "$@"; do
  if [ "$argument" = "-encoders" ]; then
    echo " A..... aac_at AAC AudioToolbox"
    exit 0
  fi
  if [ "$previous" = "-c:a" ] && [ "$argument" = "aac_at" ]; then
    echo "AudioToolbox unavailable in test" >&2
    exit 1
  fi
  previous="$argument"
done
exec "$SZJM_TEST_FFMPEG" "$@"
`
	if err = os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SZJM_TEST_FFMPEG", realFFmpeg)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SZJM_AAC_ENCODER", "auto")
	raw, _ := os.ReadFile("testdata/key.json")
	key, _ := ReadKey(raw)
	warned := false
	_, err = Run(context.Background(), "testdata/encrypted.sz", filepath.Join(dir, "output.mp4"), key, "", func(level, message string) {
		if level == "WARN" && strings.Contains(message, "切换至兼容编码器") {
			warned = true
		}
	}, nil)
	if err != nil || !warned {
		t.Fatalf("fallback did not produce a validated output: warned=%v err=%v", warned, err)
	}
}
