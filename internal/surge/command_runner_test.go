package surge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func flakyCLI(t *testing.T, failures int) (string, func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Surge CLI is a macOS subprocess")
	}
	dir := t.TempDir()
	countFile := filepath.Join(dir, "calls")
	t.Setenv("BINGGAN_TEST_CLI_CALLS", countFile)
	t.Setenv("BINGGAN_TEST_CLI_FAILURES", strconv.Itoa(failures))
	path := filepath.Join(dir, "surge-cli")
	script := `#!/bin/sh
set -eu
n=0
if [ -f "$BINGGAN_TEST_CLI_CALLS" ]; then n=$(cat "$BINGGAN_TEST_CLI_CALLS"); fi
n=$((n+1))
printf '%s' "$n" > "$BINGGAN_TEST_CLI_CALLS"
if [ "$n" -le "$BINGGAN_TEST_CLI_FAILURES" ]; then
 printf 'private-profile-password' >&2
 exit 1
fi
printf '{"policy":"Binggan-Shenzao-Capture"}'
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path, func() int { b, _ := os.ReadFile(countFile); n, _ := strconv.Atoi(string(b)); return n }
}

func TestCommandRunnerRecoversTransientReadOnlyFailure(t *testing.T) {
	path, calls := flakyCLI(t, 2)
	out, err := commandRunner(path)(context.Background(), "--raw", "rule", "match", "www.shenzaokeji.com")
	if err != nil || !strings.Contains(string(out), `"policy":"Binggan-Shenzao-Capture"`) || calls() != 3 {
		t.Fatalf("read-only query did not recover: calls=%d err=%v", calls(), err)
	}
}

func TestCommandRunnerNeverRepeatsMutatingCommands(t *testing.T) {
	for _, args := range [][]string{{"reload"}, {"rule", "temp", "add", "private-rule"}, {"rule", "temp", "remove", "private-rule"}, {"--check", "private-profile"}} {
		t.Run(strings.Join(args[:1], ""), func(t *testing.T) {
			path, calls := flakyCLI(t, 2)
			_, err := commandRunner(path)(context.Background(), args...)
			if err == nil || calls() != 1 {
				t.Fatalf("mutation was repeated: calls=%d err=%v", calls(), err)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("private arguments or stderr leaked")
			}
		})
	}
}

func TestCommandRunnerBoundsRetriesAndKeepsSafeDiagnostics(t *testing.T) {
	path, calls := flakyCLI(t, 99)
	_, err := commandRunner(path)(context.Background(), "--raw", "rule", "match", "private-host")
	if err == nil || calls() != 3 {
		t.Fatalf("permanent failure retry count: %d err=%v", calls(), err)
	}
	if !strings.Contains(err.Error(), "rule match") || !strings.Contains(err.Error(), "退出码 1") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("missing safe command/exit diagnostics or private data leaked: %v", err)
	}
}

func TestCommandRunnerCancellationStopsReadOnlyRetry(t *testing.T) {
	path, calls := flakyCLI(t, 99)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cancelled := make(chan struct{})
	go func() {
		defer close(cancelled)
		for calls() == 0 && ctx.Err() == nil {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	_, err := commandRunner(path)(ctx, "--raw", "rule", "match", "private-host")
	<-cancelled
	if !errors.Is(err, context.Canceled) || calls() != 1 {
		t.Fatalf("cancellation did not stop retries: calls=%d err=%v", calls(), err)
	}
}
