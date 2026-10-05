package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func lifecycleFixture() (*LifecycleService, *atomic.Int32, *atomic.Int32) {
	requests, quits := &atomic.Int32{}, &atomic.Int32{}
	s := &LifecycleService{request: func(uint64) { requests.Add(1) }, quit: func() { quits.Add(1) }, report: func(string, func()) {}}
	return s, requests, quits
}

func TestQuitWaitsForRendererAndCoalescesRequests(t *testing.T) {
	s, requests, quits := lifecycleFixture()
	s.RendererReady()
	if s.allowQuit() || s.allowQuit() || requests.Load() != 1 {
		t.Fatal("quit must wait, once")
	}
	s.FinishQuit(99, "")
	if quits.Load() != 0 {
		t.Fatal("stale acknowledgement quit application")
	}
	s.FinishQuit(1, "")
	if quits.Load() != 1 || !s.allowQuit() {
		t.Fatal("saved acknowledgement did not release quit")
	}
	s.FinishQuit(1, "")
	if quits.Load() != 1 {
		t.Fatal("duplicate acknowledgement quit again")
	}
}

func TestQuitFailureRejectsLateSuccessAndCanRetry(t *testing.T) {
	s, requests, quits := lifecycleFixture()
	shown, dismiss := make(chan struct{}), make(chan struct{})
	s.report = func(string, func()) { close(shown); <-dismiss }
	s.RendererReady()
	s.allowQuit()
	s.FinishQuit(1, "save timeout")
	<-shown
	s.FinishQuit(1, "")
	if s.allowQuit() || quits.Load() != 0 || requests.Load() != 1 {
		t.Fatal("late success bypassed warning")
	}
	close(dismiss)
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		pending := s.pending
		s.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not release request")
		}
		time.Sleep(time.Millisecond)
	}
	if s.allowQuit() || requests.Load() != 2 {
		t.Fatal("cannot retry after staying")
	}
	s.FinishQuit(2, "")
	if quits.Load() != 1 {
		t.Fatal("retry failed")
	}
}

func TestQuitForceRequiresExplicitWarningChoice(t *testing.T) {
	s, _, quits := lifecycleFixture()
	done := make(chan struct{})
	s.report = func(_ string, force func()) { force(); close(done) }
	s.RendererReady()
	s.allowQuit()
	s.FinishQuit(1, "pause failed")
	<-done
	if quits.Load() != 1 || !s.allowQuit() {
		t.Fatal("explicit force did not release quit")
	}
}

func TestClosingWindowNeverAuthorizesQuit(t *testing.T) {
	s, requests, quits := lifecycleFixture()
	if s.windowMayClose() {
		t.Fatal("startup close must hide")
	}
	s.RendererReady()
	if s.windowMayClose() || requests.Load() != 0 || quits.Load() != 0 {
		t.Fatal("window close started quit")
	}
	s.allowQuit()
	if s.windowMayClose() {
		t.Fatal("window must survive pending cleanup")
	}
	s.FinishQuit(1, "")
	if !s.windowMayClose() {
		t.Fatal("approved quit cannot close window")
	}
}

func TestExplicitQuitBeforeRendererIsReadyCanCloseWindow(t *testing.T) {
	s, _, _ := lifecycleFixture()
	if !s.allowQuit() || !s.windowMayClose() {
		t.Fatal("startup quit must not be intercepted as hide")
	}
}
