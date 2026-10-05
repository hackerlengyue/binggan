package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourcesSurviveTaskFilesAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "output.mp4")
	data := []byte("verified video")
	os.WriteFile(input, data, 0600)
	r := Resource{ID: ID(), Name: "课程.mp4", TaskID: ID(), TaskName: "已删除任务", FinishedAt: Now()}
	if err = s.PublishResource(dir, input, r); err != nil {
		t.Fatal(err)
	}
	os.Remove(input)
	if err = s.PublishResource(dir, input, r); err != nil {
		t.Fatal("import is not idempotent", err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Resource(r.ID)
	if err != nil || got.Name != r.Name || got.Size != int64(len(data)) {
		t.Fatal(got, err)
	}
	b, err := os.ReadFile(ResourcePath(dir, r.ID))
	if err != nil || string(b) != string(data) {
		t.Fatal("resource was not retained", err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM resources").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestResourceRejectsSymlinksAndInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	src := filepath.Join(dir, "video")
	os.WriteFile(src, []byte("video"), 0600)
	link := filepath.Join(dir, "link")
	os.Symlink(src, link)
	if s.PublishResource(dir, link, Resource{ID: ID()}) == nil {
		t.Fatal("symlink accepted")
	}
	if s.PublishResource(dir, src, Resource{ID: "../escape"}) == nil {
		t.Fatal("invalid id accepted")
	}
}

func TestRepublishAfterFailedAttemptUpdatesExistingResource(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(dir, "output.mp4")
	os.WriteFile(source, []byte("first attempt"), 0600)
	r := Resource{ID: ID(), Name: "video.mp4", FinishedAt: "2026-09-25T01:00:00Z"}
	if err = s.PublishResource(dir, source, r); err != nil {
		t.Fatal(err)
	}
	// Engine atomically replaces the job output on a subsequent attempt.
	os.Remove(source)
	os.WriteFile(source, []byte("second verified attempt"), 0600)
	r.FinishedAt = "2026-09-25T01:01:00Z"
	if err = s.PublishResource(dir, source, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(ResourcePath(dir, r.ID))
	if err != nil || string(b) != "second verified attempt" {
		t.Fatal("stale retry output", string(b), err)
	}
	got, err := s.Resource(r.ID)
	if err != nil || got.Size != int64(len(b)) || got.FinishedAt != r.FinishedAt {
		t.Fatal("stale retry metadata", got, err)
	}
}

func TestRepublishAfterDatabaseFailureReplacesOrphanFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(dir, "output.mp4")
	if err = os.WriteFile(source, []byte("first attempt"), 0600); err != nil {
		t.Fatal(err)
	}
	r := Resource{ID: ID(), Name: "video.mp4", FinishedAt: Now()}
	if _, err = s.DB.Exec(`CREATE TRIGGER fail_resource BEFORE INSERT ON resources BEGIN SELECT RAISE(FAIL, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishResource(dir, source, r); err == nil {
		t.Fatal("expected database failure")
	}
	if _, err = os.Stat(ResourcePath(dir, r.ID)); err != nil {
		t.Fatal("expected retained file", err)
	}
	if _, err = s.DB.Exec("DROP TRIGGER fail_resource"); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	want := "second verified attempt"
	if err = os.WriteFile(source, []byte(want), 0600); err != nil {
		t.Fatal(err)
	}
	r.FinishedAt = Now()
	if err = s.PublishResource(dir, source, r); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(ResourcePath(dir, r.ID))
	if err != nil || string(got) != want {
		t.Fatalf("stale orphan output: %q, %v", got, err)
	}
	record, err := s.Resource(r.ID)
	if err != nil || record.Size != int64(len(want)) {
		t.Fatal("incorrect resource metadata", record, err)
	}
}
