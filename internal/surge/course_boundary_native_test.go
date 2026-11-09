package surge

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestNativeSurgeRepeatedCaptureSessions(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("BINGGAN_SURGE_INTEGRATION") != "1" {
		t.Skip("requires native Surge integration")
	}
	ctx := context.Background()
	active, err := Active(ctx)
	if err != nil || !active {
		t.Fatalf("active Surge required: %v", err)
	}
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, err := m.current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path, err := m.profilePath(profile)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Disable(ctx); err != nil {
			t.Error("restore failed:", err)
		}
		current, err := os.ReadFile(path)
		if err != nil || string(current) != string(original) {
			t.Error("profile was not restored byte-for-byte")
		}
	})
	// Log only fixed command words, timings and results. Surge's raw
	// stderr and configuration arguments may contain credentials.
	run := m.run
	m.run = func(ctx context.Context, args ...string) ([]byte, error) {
		started := time.Now()
		out, err := run(ctx, args...)
		operation, _ := surgeCommand(args)
		t.Logf("command=%s elapsed=%s success=%t", operation, time.Since(started), err == nil)
		return out, err
	}
	d := descriptor(t)
	for i := 0; i < 12; i++ {
		t.Logf("capture session %d", i+1)
		if err := m.Enable(ctx, d); err != nil {
			t.Fatalf("session %d enable: %v", i+1, err)
		}
		if err := m.Disable(ctx); err != nil {
			t.Fatalf("session %d disable: %v", i+1, err)
		}
	}
}
