package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/cachefile"
	boxlog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common/logger"
)

func TestHelperCacheWorksFromReadOnlyWorkingDirectory(t *testing.T) {
	runtimeDir := t.TempDir()
	readOnlyDir := t.TempDir()
	if err := os.Chmod(readOnlyDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readOnlyDir, 0700) })
	t.Chdir(readOnlyDir)
	_, opts, err := helperOptions(descriptor(t), runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Experimental == nil || opts.Experimental.CacheFile == nil {
		t.Fatal("helper uses the implicit relative cache path")
	}
	cache := cachefile.New(context.Background(), logger.NOP(), *opts.Experimental.CacheFile)
	defer cache.Close()
	if err := cache.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "cache.db")); err != nil {
		t.Fatal("runtime cache was not created:", err)
	}
	if _, err := os.Stat(filepath.Join(readOnlyDir, "cache.db")); !os.IsNotExist(err) {
		t.Fatalf("cache leaked into working directory: %v", err)
	}
}

func TestHelperLogLevelsDoNotReportTraceAsErrors(t *testing.T) {
	w := &logWriter{queue: make(chan Report, 10)}
	w.WriteMessage(boxlog.LevelTrace, "close http-client")
	w.WriteMessage(boxlog.LevelDebug, "debug")
	w.WriteMessage(boxlog.LevelInfo, "info")
	w.WriteMessage(boxlog.LevelWarn, "warning")
	w.WriteMessage(boxlog.LevelError, "error")
	if len(w.queue) != 2 {
		t.Fatalf("unexpected log filtering: %d", len(w.queue))
	}
	if got := <-w.queue; got.Level != "warn" || got.Message != "warning" {
		t.Fatalf("warning mislabeled: %+v", got)
	}
	if got := <-w.queue; got.Level != "error" || got.Message != "error" {
		t.Fatalf("error mislabeled: %+v", got)
	}
}

func TestLogWriterStripsANSIAndDropsDirectNoise(t *testing.T) {
	w := &logWriter{queue: make(chan Report, 4)}
	w.WriteMessage(boxlog.LevelError, "\x1b[31mERROR\x1b[0m[1992705641] connection: open connection to 198.18.0.2:53 using outbound/direct[direct]: dial tcp 198.18.0.2:53: connect: connection refused")
	w.WriteMessage(boxlog.LevelError, "\x1b[31mERROR\x1b[0m connection: open connection to example.com:443 using outbound/capture[capture]: dial tcp 127.0.0.1:18767: connect: connection refused")
	if len(w.queue) != 1 {
		t.Fatalf("expected direct-outbound noise dropped, got %d messages", len(w.queue))
	}
	got := <-w.queue
	if strings.Contains(got.Message, "\x1b") {
		t.Fatalf("ansi escape survived: %q", got.Message)
	}
	if !strings.Contains(got.Message, "outbound/capture") {
		t.Fatalf("capture failure lost: %q", got.Message)
	}
}
