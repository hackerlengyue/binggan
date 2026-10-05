package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func videoChoice(t *testing.T, app *App, name string, data []byte) LocalVideo {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	items, err := app.InspectVideoFiles([]string{path})
	if err != nil || len(items) != 1 {
		t.Fatalf("inspect = %+v, %v", items, err)
	}
	return items[0]
}

func TestNativeVideoImportCopiesAndRechecksSource(t *testing.T) {
	app := testApp(t)
	content := bytes.Repeat([]byte("video"), 8192)
	choice := videoChoice(t, app, "课程.sz", content)
	if choice.Error != "" || choice.Size != int64(len(content)) {
		t.Fatalf("inspected file = %+v", choice)
	}
	progress := []VideoImportProgress{}
	result, err := app.ImportVideo(context.Background(), choice, func(value VideoImportProgress) error {
		progress = append(progress, value)
		return nil
	})
	if err != nil || result.Name != choice.Name || result.ID == "" {
		t.Fatalf("import result = %+v, %v", result, err)
	}
	if len(progress) < 2 || progress[0].Copied != 0 || progress[len(progress)-1].Copied != choice.Size {
		t.Fatalf("progress = %+v", progress)
	}
	upload, ok := app.Tasks.Upload(result.ID)
	if !ok {
		t.Fatal("successful import was not registered")
	}
	copied, err := os.ReadFile(upload.Path)
	if err != nil || !bytes.Equal(copied, content) {
		t.Fatalf("copied bytes = %d, %v", len(copied), err)
	}
	if err := os.WriteFile(choice.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ImportVideo(context.Background(), choice, nil); err == nil || !strings.Contains(err.Error(), "发生变化") {
		t.Fatalf("changed source accepted: %v", err)
	}
}

func TestNativeVideoImportRejectsInvalidAndCleansOnCancel(t *testing.T) {
	app := testApp(t)
	for _, sample := range []struct {
		name string
		data []byte
	}{
		{"wrong.mp4", []byte("video")},
		{"empty.sz", nil},
		{"huge.sz", bytes.Repeat([]byte("x"), 1<<20+1)},
	} {
		choice := videoChoice(t, app, sample.name, sample.data)
		if choice.Error == "" {
			t.Fatalf("invalid file accepted: %+v", choice)
		}
		if _, err := app.ImportVideo(context.Background(), choice, nil); err == nil {
			t.Fatalf("import accepted invalid %s", sample.name)
		}
	}
	actual := videoChoice(t, app, "actual.sz", []byte("video"))
	link := filepath.Join(t.TempDir(), "linked.sz")
	if err := os.Symlink(actual.Path, link); err != nil {
		t.Fatal(err)
	}
	items, err := app.InspectVideoFiles([]string{link})
	if err != nil || len(items) != 1 || items[0].Error == "" {
		t.Fatalf("symlink accepted: %+v, %v", items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	before, err := os.ReadDir(app.Tasks.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.ImportVideo(ctx, actual, func(VideoImportProgress) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import = %v", err)
	}
	after, err := os.ReadDir(app.Tasks.Dir)
	if err != nil || len(after) != len(before) {
		t.Fatalf("cancellation left task files: %d -> %d, %v", len(before), len(after), err)
	}
}

func TestNativeVideoImportLimitsConcurrentCopies(t *testing.T) {
	app := testApp(t)
	choice := videoChoice(t, app, "parallel.sz", []byte("video"))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := app.ImportVideo(context.Background(), choice, func(value VideoImportProgress) error {
				if value.Copied == 0 {
					entered <- struct{}{}
					<-release
				}
				return nil
			})
			done <- err
		}()
	}
	<-entered
	<-entered
	if _, err := app.ImportVideo(context.Background(), choice, nil); err == nil || !strings.Contains(err.Error(), "繁忙") {
		t.Fatalf("third import = %v", err)
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
