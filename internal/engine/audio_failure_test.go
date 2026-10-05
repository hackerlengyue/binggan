package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScanAudioCorruptPacketDiagnostics(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "corrupt.m4a")
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=1000:duration=2", "-c:a", "aac", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	// Verify a healthy input before corrupting exactly one container sample.
	if _, _, err := ScanAudio(ctx, path, nil, nil); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	samples, err := Samples(f, info.Size())
	if err != nil || len(samples) < 20 {
		t.Fatalf("samples: %d %v", len(samples), err)
	}
	packet := samples[len(samples)/2]
	// 0xff begins an invalid AAC payload while retaining valid MP4 tables.
	damaged := make([]byte, packet.Size)
	for i := range damaged {
		damaged[i] = 0xff
	}
	if _, err = f.WriteAt(damaged, packet.Offset); err != nil {
		t.Fatal(err)
	}
	var lastDone, lastTotal float64
	var decoderLog strings.Builder
	var logMu sync.Mutex
	scan, _, err := ScanAudio(ctx, path, func(level, msg string) {
		if level == "STDERR" {
			logMu.Lock()
			decoderLog.WriteString(msg)
			logMu.Unlock()
		}
	}, func(done, total float64) { lastDone, lastTotal = done, total })
	if err == nil {
		t.Fatal("corrupt AAC accepted")
	}
	if scan.Frames == 0 || lastDone <= 0 || lastDone >= lastTotal {
		t.Errorf("failed scan reported completion: frames=%d progress=%v/%v", scan.Frames, lastDone, lastTotal)
	}
	if !strings.Contains(decoderLog.String(), "[aac @") {
		t.Fatalf("fixture did not produce AAC error: %s", decoderLog.String())
	}
	if !strings.Contains(err.Error(), "[aac @") || !strings.Contains(err.Error(), "预期帧时间") {
		t.Errorf("missing decoder cause or frame context: %v", err)
	}
}
