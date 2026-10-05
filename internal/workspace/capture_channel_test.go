package workspace

import (
	"context"
	"errors"
	"testing"
	"time"
)

func receiveNotice(t *testing.T, notices <-chan CaptureNotice) CaptureNotice {
	t.Helper()
	select {
	case notice := <-notices:
		return notice
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for capture notice")
		return CaptureNotice{}
	}
}

func waitSubscribers(t *testing.T, app *App, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if app.hub.count() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscribers = %d, want %d", app.hub.count(), want)
}

func TestCaptureChannelSnapshotEventsAndCancellation(t *testing.T) {
	app := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notices := make(chan CaptureNotice, 8)
	done := make(chan error, 1)
	go func() {
		done <- app.Subscribe(ctx, func(notice CaptureNotice) error {
			notices <- notice
			return nil
		})
	}()
	if got := receiveNotice(t, notices); got.Kind != "ready" {
		t.Fatalf("first notice = %q", got.Kind)
	}
	if got := receiveNotice(t, notices); got.Kind != "capture_state" || got.Enabled == nil || *got.Enabled {
		t.Fatalf("initial state = %+v", got)
	}
	if !app.SetCaptureState(true) {
		t.Fatal("capture did not turn on")
	}
	if got := receiveNotice(t, notices); got.Kind != "capture_state" || got.Enabled == nil || !*got.Enabled {
		t.Fatalf("updated state = %+v", got)
	}
	app.hub.broadcast(CaptureNotice{Kind: "message", Capture: &Capture{ID: "sample"}})
	if got := receiveNotice(t, notices); got.Kind != "message" || got.Capture == nil || got.Capture.ID != "sample" {
		t.Fatalf("capture message = %+v", got)
	}
	if err := app.ClearTraces("window-1"); !errors.Is(err, errCaptureRunning) {
		t.Fatalf("clear while capturing = %v", err)
	}
	if app.SetCaptureState(false) {
		t.Fatal("capture did not turn off")
	}
	if got := receiveNotice(t, notices); got.Kind != "capture_state" || got.Enabled == nil || *got.Enabled {
		t.Fatalf("stop notice after rejected clear = %+v", got)
	}
	if err := app.ClearTraces("window-1"); err != nil {
		t.Fatal(err)
	}
	if got := receiveNotice(t, notices); got.Kind != "clear" || got.ClientID != "window-1" {
		t.Fatalf("clear notice = %+v", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop subscription")
	}
	waitSubscribers(t, app, 0)
}

func TestCaptureChannelClientLimitAndSlowConsumer(t *testing.T) {
	app := testApp(t)
	var cancels []context.CancelFunc
	for range 32 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		ready := make(chan struct{}, 1)
		go func() {
			_ = app.Subscribe(ctx, func(notice CaptureNotice) error {
				if notice.Kind == "ready" {
					ready <- struct{}{}
				}
				return nil
			})
		}()
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("subscriber did not start")
		}
	}
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	if err := app.Subscribe(context.Background(), func(CaptureNotice) error { return nil }); !errors.Is(err, errSubscriberLimit) {
		t.Fatalf("33rd subscriber error = %v", err)
	}
	cancels[0]()
	waitSubscribers(t, app, 31)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- app.Subscribe(ctx, func(notice CaptureNotice) error {
			if notice.Kind == "ready" {
				ready <- struct{}{}
			}
			return nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("slot was not released after cancellation")
	}
	cancel()
	<-done
	for _, stop := range cancels {
		stop()
	}
	waitSubscribers(t, app, 0)

	ch, ok := app.hub.add()
	if !ok {
		t.Fatal("failed to add slow test client")
	}
	for range 129 {
		app.hub.broadcast(CaptureNotice{Kind: "message"})
	}
	waitSubscribers(t, app, 0)
	for range 128 {
		if _, open := <-ch; !open {
			t.Fatal("slow client closed before draining its buffered messages")
		}
	}
	if _, open := <-ch; open {
		t.Fatal("slow client remained open after buffer overflow")
	}
}
