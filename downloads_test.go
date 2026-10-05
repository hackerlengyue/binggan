package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestDownloadSaveIsChosenAndPublishedOnlyAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(dest, []byte("old video"), 0o600); err != nil {
		t.Fatal(err)
	}
	var reported []error
	m := &downloadManager{
		choose: func(opts mygo.SaveDialogOptions) (string, error) {
			if opts.Title != "保存文件" || filepath.Base(opts.DefaultPath) != "video.mp4" {
				t.Errorf("unexpected save dialog options: %+v", opts)
			}
			return dest, nil
		},
		report:  func(err error) { reported = append(reported, err) },
		pending: make(map[string]string),
	}
	event := &mygo.DownloadEvent{SuggestedName: "video.mp4"}
	m.willDownload(event)
	if event.DefaultPrevented() || event.Path == "" || event.Path == dest {
		t.Fatalf("download should use a separate staging path: %+v", event)
	}
	if b, _ := os.ReadFile(dest); string(b) != "old video" {
		t.Fatalf("previous destination changed before completion: %q", b)
	}
	if err := os.WriteFile(event.Path, []byte("new video"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.downloadDone(&mygo.Download{Path: event.Path})
	if b, _ := os.ReadFile(dest); string(b) != "new video" {
		t.Fatalf("completed download bytes = %q", b)
	}
	if _, err := os.Stat(event.Path); !os.IsNotExist(err) {
		t.Fatalf("staging file remains: %v", err)
	}
	if len(reported) != 0 || len(m.pending) != 0 {
		t.Fatalf("unexpected error or pending download: %v, %v", reported, m.pending)
	}
}

func TestDownloadCancelAndFailedTransferLeaveExistingFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "logs.txt")
	if err := os.WriteFile(dest, []byte("old logs"), 0o600); err != nil {
		t.Fatal(err)
	}
	var reported []error
	m := &downloadManager{
		choose:  func(mygo.SaveDialogOptions) (string, error) { return "", nil },
		report:  func(err error) { reported = append(reported, err) },
		pending: make(map[string]string),
	}
	event := &mygo.DownloadEvent{SuggestedName: "logs.txt"}
	m.willDownload(event)
	if !event.DefaultPrevented() || len(m.pending) != 0 || len(reported) != 0 {
		t.Fatalf("canceled save changed state: %+v, %v", event, reported)
	}
	m.choose = func(mygo.SaveDialogOptions) (string, error) { return dest, nil }
	event = &mygo.DownloadEvent{SuggestedName: "logs.txt"}
	m.willDownload(event)
	if err := os.WriteFile(event.Path, []byte("partial logs"), 0o600); err != nil {
		t.Fatal(err)
	}
	transferErr := errors.New("injected transfer failure")
	m.downloadDone(&mygo.Download{Path: event.Path, Err: transferErr})
	if b, _ := os.ReadFile(dest); string(b) != "old logs" {
		t.Fatalf("failed transfer changed destination: %q", b)
	}
	if _, err := os.Stat(event.Path); !os.IsNotExist(err) {
		t.Fatalf("failed transfer left staging file: %v", err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], transferErr) {
		t.Fatalf("transfer failure not reported: %v", reported)
	}
	m.choose = func(mygo.SaveDialogOptions) (string, error) { return "", errors.New("dialog unavailable") }
	event = &mygo.DownloadEvent{SuggestedName: "logs.txt"}
	m.willDownload(event)
	if !event.DefaultPrevented() || len(m.pending) != 0 || len(reported) != 2 {
		t.Fatalf("dialog error did not cancel and report: %+v, %v", event, reported)
	}
	m.choose = func(mygo.SaveDialogOptions) (string, error) { return dest, nil }
	event = &mygo.DownloadEvent{SuggestedName: "logs.txt"}
	m.willDownload(event)
	if err := os.WriteFile(event.Path, []byte("interrupted transfer"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.close()
	if b, _ := os.ReadFile(dest); string(b) != "old logs" {
		t.Fatalf("quit during transfer changed destination: %q", b)
	}
	if _, err := os.Stat(event.Path); !os.IsNotExist(err) || len(m.pending) != 0 {
		t.Fatalf("quit left staging file or pending state: %v, %v", err, m.pending)
	}
}

func TestDownloadCannotPrepareDestination(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "missing", "export.txt")
	var reported error
	m := &downloadManager{
		choose:  func(mygo.SaveDialogOptions) (string, error) { return dest, nil },
		report:  func(err error) { reported = err },
		pending: make(map[string]string),
	}
	event := &mygo.DownloadEvent{SuggestedName: "export.txt"}
	m.willDownload(event)
	if !event.DefaultPrevented() || event.Path != "" || len(m.pending) != 0 || reported == nil {
		t.Fatalf("unwritable destination was not canceled: %+v, pending=%v, error=%v", event, m.pending, reported)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination unexpectedly exists: %v", err)
	}
}

func TestFailedAttachmentWithoutSaveDestinationIsReported(t *testing.T) {
	var reported error
	m := &downloadManager{
		report:  func(err error) { reported = err },
		pending: make(map[string]string),
	}
	failed := errors.New("source file missing")
	m.downloadDone(&mygo.Download{Err: failed})
	if !errors.Is(reported, failed) || len(m.pending) != 0 {
		t.Fatalf("failed attachment was not reported: %v, pending=%v", reported, m.pending)
	}
}
