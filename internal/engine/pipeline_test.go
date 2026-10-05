package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mediaFixture(t *testing.T) (context.Context, string, Scan, AudioStream) {
	t.Helper()
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	b, err := os.ReadFile("testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	k, err := ReadKey(b)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "media.mp4")
	if _, err = Run(ctx, "testdata/encrypted.sz", path, k, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	scan, stream, err := ScanAudio(ctx, path, nil, nil)
	if err != nil || stream == nil {
		t.Fatalf("fixture audio: %v", err)
	}
	return ctx, path, scan, *stream
}

func TestStreamingRepairCancellation(t *testing.T) {
	toolsAvailable(t)
	realFFmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	wrappers := t.TempDir()
	ready := filepath.Join(wrappers, "ready")
	// Keep the encoder's input open without consuming it. The repaired PCM
	// exceeds the bounded pipe, so cancellation must release a blocked writer.
	wrapper := `#!/bin/sh
previous=""
for argument in "$@"; do
  if [ "$previous" = "-c:a" ] && [ "$argument" = "aac" ]; then
    touch "$SZJM_TEST_READY"
    exec sleep 30
  fi
  previous="$argument"
done
exec "$SZJM_TEST_FFMPEG" "$@"
`
	if err = os.WriteFile(filepath.Join(wrappers, "ffmpeg"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SZJM_TEST_READY", ready)
	t.Setenv("SZJM_TEST_FFMPEG", realFFmpeg)
	t.Setenv("PATH", wrappers+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SZJM_AAC_ENCODER", "aac")
	b, _ := os.ReadFile("testdata/key.json")
	k, _ := ReadKey(b)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, "testdata/encrypted.sz", filepath.Join(dir, "output.mp4"), k, "", nil, nil)
		done <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
waitReady:
	for {
		select {
		case err := <-done:
			t.Fatalf("pipeline ended before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("encoder did not start")
		case <-ticker.C:
			if _, err := os.Stat(ready); err == nil {
				break waitReady
			}
		}
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled pipeline left a blocked producer or encoder")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled task retained output or temporary files: %v %v", entries, err)
	}
}

func TestStreamingRepairFailureDoesNotRetryEncoder(t *testing.T) {
	ctx, path, scan, stream := mediaFixture(t)
	scan.First = scan.End + 1 // Every source frame now violates the expected PTS.
	warned := false
	_, err := RestoreAudio(ctx, path, filepath.Join(t.TempDir(), "failed.mp4"), stream, scan, 4, func(level, _ string) {
		if level == "WARN" {
			warned = true
		}
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "时间戳重叠") || warned {
		t.Fatalf("repair error hidden or retried as encoder failure: %v warned=%v", err, warned)
	}
}

func TestParallelValidationRejectsCorruptVideo(t *testing.T) {
	ctx, path, before, _ := mediaFixture(t)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, _ := f.Stat()
	samples, err := Samples(f, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		if sample.Video && sample.Size > 32 {
			// Leave the container intact while corrupting compressed video data.
			if _, err = f.WriteAt(bytes.Repeat([]byte{0xff}, sample.Size-5), sample.Offset+5); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	_, _, err = validateMedia(ctx, path, before, true, nil, nil)
	if err == nil {
		t.Fatal("parallel validation accepted corrupt video with valid audio")
	}
}

func TestParallelValidationPreservesAudioChecks(t *testing.T) {
	ctx, path, before, _ := mediaFixture(t)
	t.Run("timing", func(t *testing.T) {
		shifted := before
		shifted.First += 1
		if _, _, err := validateMedia(ctx, path, shifted, true, nil, nil); err == nil || !strings.Contains(err.Error(), "起止时间") {
			t.Fatalf("audio time mismatch accepted: %v", err)
		}
	})
	t.Run("energy", func(t *testing.T) {
		louder := before
		louder.Seconds = append([]Second(nil), before.Seconds...)
		for i := range louder.Seconds {
			louder.Seconds[i].RMS *= 1000
		}
		if _, loss, err := validateMedia(ctx, path, louder, true, nil, nil); err == nil || len(loss) == 0 {
			t.Fatalf("audio energy loss accepted: %v", err)
		}
	})
}
