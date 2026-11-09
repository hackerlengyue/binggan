package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/surge"
)

type nativeQueueLease struct {
	manager    *surge.Manager
	descriptor capture.Descriptor
	calls      []bool
}

func (l *nativeQueueLease) State() (string, string) { return "ready", "" }
func (l *nativeQueueLease) Stop() error             { return l.manager.Disable(context.Background()) }
func (l *nativeQueueLease) SetCaptureEnabled(ctx context.Context, enabled bool) error {
	l.calls = append(l.calls, enabled)
	if enabled {
		return l.manager.Enable(ctx, l.descriptor)
	}
	return l.manager.Disable(ctx)
}

// Exercise the actual monitor/receiver boundary with the running Surge. No TUN
// is started and the original local profile must be restored byte-for-byte.
func TestNativeQueueKeepsSurgeRoutingAcrossCourses(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("BINGGAN_SURGE_INTEGRATION") != "1" {
		t.Skip("requires native Surge integration")
	}
	const cli = "/Applications/Surge.app/Contents/Applications/surge-cli"
	out, err := exec.Command(cli, "--raw", "profile", "current").Output()
	if err != nil {
		t.Fatal("cannot inspect active profile")
	}
	var active struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(out, &active) != nil || active.Profile == "" {
		t.Fatal("invalid active profile")
	}
	home, _ := os.UserHomeDir()
	var path string
	for _, dir := range []string{filepath.Join(home, "Library/Application Support/Surge/Profiles"), filepath.Join(home, "Library/Mobile Documents/iCloud~com~nssurge~inc~Surge/Documents")} {
		candidate := filepath.Join(dir, active.Profile+".conf")
		if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() {
			path = candidate
			break
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read active profile")
	}
	manager, err := surge.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Disable(context.Background()); err != nil {
			t.Error(err)
		}
	})
	m, s, _ := routingFixture(t)
	lease := &nativeQueueLease{manager: manager, descriptor: testDescriptor(t)}
	m.startTunnel = func(context.Context, capture.Descriptor, *capture.Logbook) (captureLease, error) { return lease, nil }
	owner, release := startRoutingHold(t, m, s)
	var held string
	for i := 0; i < 3; i++ {
		if _, err := s.SetCaptureState(context.Background(), true, owner); err != nil {
			t.Fatal(err)
		}
		if err := manager.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetCaptureState(context.Background(), false, owner); err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			held = string(current)
		} else if string(current) != held {
			t.Fatal("course boundary rewrote Surge profile")
		}
		if !reflect.DeepEqual(lease.calls, []bool{true}) {
			t.Fatalf("per-course route mutation: %v", lease.calls)
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("original profile not restored")
	}
	if !reflect.DeepEqual(lease.calls, []bool{true, false}) {
		t.Fatal(lease.calls)
	}
	t.Log("3 course captures: one route setup, one restore; receiver disabled between records")
}
