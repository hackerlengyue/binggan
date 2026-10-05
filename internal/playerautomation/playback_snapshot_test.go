package playerautomation

import (
	"errors"
	"reflect"
	"testing"
)

func playbackFixture(percent, clock string) Snapshot {
	return snapshot(true, []Element{
		{Role: "AXSlider", Identifier: "_NS:26", Value: percent},
		{Role: "AXStaticText", Identifier: "_NS:8", Value: clock},
	})
}

func TestPlaybackSnapshotPreservesNativeFinishMarker(t *testing.T) {
	// Reconstructed from PlayerControlBar's finish observer and showMediaBar:
	// finish writes 100/duration; showing the bar recomputes the last frame.
	terminal := playbackFixture("100.000", "00:00:04/00:00:04")
	current := terminal
	refreshes := 0
	got, err := refreshedPlaybackSnapshot(func() (Snapshot, error) {
		return current, nil
	}, func() error {
		refreshes++
		current = playbackFixture("99.000", "00:00:04/00:00:04")
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, terminal) || refreshes != 0 {
		t.Fatalf("native finish marker was overwritten: got=%+v refreshes=%d err=%v", got, refreshes, err)
	}
}

func TestPlaybackSnapshotRefreshesNonterminalControls(t *testing.T) {
	for _, initial := range []Snapshot{
		playbackFixture("0.000", "00:00:00/00:00:00"),
		playbackFixture("25.000", "00:00:01/00:00:04"),
		playbackFixture("99.000", "00:00:04/00:00:04"),
		snapshot(true, nil),
	} {
		current := initial
		updated := playbackFixture("75.000", "00:00:03/00:00:04")
		var events []string
		got, err := refreshedPlaybackSnapshot(func() (Snapshot, error) {
			events = append(events, "read")
			return current, nil
		}, func() error {
			events = append(events, "refresh")
			current = updated
			return nil
		})
		if err != nil || !reflect.DeepEqual(got, updated) || !reflect.DeepEqual(events, []string{"read", "refresh", "read"}) {
			t.Fatalf("stale progress not refreshed: got=%+v events=%v err=%v", got, events, err)
		}
	}
}

func TestPlaybackSnapshotDoesNotRefreshFailedOrClosedReads(t *testing.T) {
	failure := errors.New("AX unavailable")
	for _, state := range []struct {
		current Snapshot
		err     error
	}{{snapshot(false, nil), nil}, {snapshot(true, nil), failure}} {
		_, err := refreshedPlaybackSnapshot(func() (Snapshot, error) {
			return state.current, state.err
		}, func() error {
			t.Fatal("must not send input after a failed or closed read")
			return nil
		})
		if !errors.Is(err, state.err) {
			t.Fatalf("read error lost: %v", err)
		}
	}
	_, err := refreshedPlaybackSnapshot(func() (Snapshot, error) {
		return snapshot(true, nil), nil
	}, func() error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("refresh error lost: %v", err)
	}
}

func TestPlaybackCompletionMarkerRequiresBothVerifiedControls(t *testing.T) {
	if !hasPlaybackCompletionMarker(playbackFixture("100.000", "01:10:00 / 01:10:00")) {
		t.Fatal("valid native finish marker was rejected")
	}
	for _, invalid := range []Snapshot{
		playbackFixture("99.999", "00:00:04/00:00:04"),
		playbackFixture("100", "00:00:03/00:00:04"),
		playbackFixture("100", "00:00:00/00:00:00"),
		playbackFixture("NaN", "00:00:04/00:00:04"),
		playbackFixture("100", "00:99:04/00:99:04"),
		snapshot(true, []Element{
			{Role: "AXSlider", Identifier: "_NS:68", Value: "100"},
			{Role: "AXStaticText", Identifier: "_NS:8", Value: "00:00:04/00:00:04"},
		}),
		snapshot(true, []Element{
			{Role: "AXSlider", Identifier: "_NS:26", Value: "100"},
			{Role: "AXStaticText", Value: "00:00:04/00:00:04"},
		}),
	} {
		if hasPlaybackCompletionMarker(invalid) {
			t.Fatalf("nonterminal controls treated as native finish: %+v", invalid)
		}
	}
}

func TestPlaybackSnapshotRetainsDialogsButMarksTransientRefreshUnusable(t *testing.T) {
	current := playbackFixture("25", "00:00:01/00:00:04")
	current.Elements = append(current.Elements, Element{Role: "AXButton", Title: "登录"})
	got, err := refreshedPlaybackSnapshot(func() (Snapshot, error) { return current, nil }, func() error { return retryableRead("fullscreen_transition", "全屏切换") })
	if err != nil || got.ReadIssue == nil || got.ReadIssue.Code != "fullscreen_transition" || !reflect.DeepEqual(got.Elements, current.Elements) {
		t.Fatalf("retryable read lost state or became fatal: %+v, %v", got, err)
	}
}

func TestPlaybackSnapshotDoesNotRefreshPartialReads(t *testing.T) {
	current := playbackFixture("100", "00:00:04/00:00:04")
	current.ReadIssue = &ReadIssue{Code: "accessibility_busy", Message: "timeout"}
	got, err := refreshedPlaybackSnapshot(func() (Snapshot, error) { return current, nil }, func() error { t.Fatal("partial read must not post input"); return nil })
	if err != nil || !reflect.DeepEqual(current, got) {
		t.Fatalf("partial read lost: %+v %v", got, err)
	}
}

func TestPlaybackSnapshotNeverDowngradesPermissionOrProfileErrors(t *testing.T) {
	for _, failure := range []error{ErrPermission, ErrUnverified, ErrUnsupported} {
		_, err := refreshedPlaybackSnapshot(func() (Snapshot, error) { return snapshot(true, nil), failure }, func() error { t.Fatal("must not refresh"); return nil })
		if !errors.Is(err, failure) {
			t.Fatalf("permanent error was hidden: %v", err)
		}
	}
}

func TestPlaybackCompletionMarkerRejectsConflictingWindows(t *testing.T) {
	terminal := playbackFixture("100", "00:00:04/00:00:04")
	for _, conflicting := range []Element{
		{Role: "AXSlider", Identifier: "_NS:26", Value: "20"},
		{Role: "AXStaticText", Identifier: "_NS:8", Value: "00:00:03/00:00:04"},
		{Role: "AXStaticText", Identifier: "_NS:8", Value: "00:00:05/00:00:05"},
	} {
		current := terminal
		current.Elements = append(append([]Element{}, terminal.Elements...), conflicting)
		if hasPlaybackCompletionMarker(current) {
			t.Fatalf("conflicting controls accepted: %+v", current)
		}
	}
	if hasPlaybackCompletionMarker(playbackFixture("100", "00:004:00/00:004:00")) {
		t.Fatal("malformed clock accepted")
	}
}

func TestWindowedProgressReadsWithoutMouseEventsOrVisibilityGate(t *testing.T) {
	current := playbackFixture("25", "00:00:01/00:00:04")
	current.WindowMode = "windowed"
	reads, refreshes := 0, 0
	got, err := refreshedPlaybackSnapshot(func() (Snapshot, error) { reads++; return current, nil }, func() error {
		refreshes++
		return retryableRead("window_not_visible", "another Space")
	})
	if err != nil || !reflect.DeepEqual(got, current) || reads != 1 || refreshes != 0 {
		t.Fatalf("windowed progress incorrectly depended on mouse/visibility: reads=%d refreshes=%d issue=%+v err=%v", reads, refreshes, got.ReadIssue, err)
	}
}
