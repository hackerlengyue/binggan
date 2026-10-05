package playerautomation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitIsBoundedAndCanceledWithCallingPage(t *testing.T) {
	for _, delay := range []int{-1, 30001} {
		if Wait(context.Background(), delay) == nil {
			t.Fatalf("accepted %d", delay)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := Wait(ctx, 30000); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("canceled page left a native timer pending")
	}
	started = time.Now()
	if err := Wait(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 10*time.Millisecond {
		t.Fatal("wait returned early")
	}
}
