package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/store"
)

func TestCloseCancelsBeforeWaitingForTaskLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	close(m.done)
	m.mu.Lock()
	finished := make(chan struct{})
	go func() { m.Close(); close(finished) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("shutdown waited for the task lock before cancelling input copy")
	}
	m.mu.Unlock()
	<-finished
}

func TestRetainInputHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{Dir: root, ctx: ctx}
	id := store.ID()
	if err := os.Mkdir(m.Path(id, ""), 0700); err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err := m.retainInput(id, Prepared{Input: "../engine/testdata/encrypted.sz"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input retention = %v", err)
	}
	if _, err := os.Stat(m.Path(id, "request.json")); !os.IsNotExist(err) {
		t.Fatalf("cancelled request persisted: %v", err)
	}
}

func TestPersistenceFailureTerminatesJobAndCleansOwnedFiles(t *testing.T) {
	for _, tc := range []struct {
		name, trigger, want string
		durable             bool
	}{
		{"start", "CREATE TRIGGER fail_update BEFORE UPDATE ON jobs WHEN json_extract(NEW.payload,'$.status')='running' BEGIN SELECT RAISE(FAIL,'injected start failure'); END", "状态", true},
		{"progress", "CREATE TRIGGER fail_update BEFORE UPDATE ON jobs WHEN json_extract(OLD.payload,'$.status')='running' AND json_extract(NEW.payload,'$.status')='running' BEGIN SELECT RAISE(FAIL,'injected progress failure'); END", "进度", true},
		{"completion", "CREATE TRIGGER fail_update BEFORE UPDATE ON jobs WHEN json_extract(NEW.payload,'$.status')='completed' BEGIN SELECT RAISE(FAIL,'injected completion failure'); END", "状态", true},
		{"all_updates", "CREATE TRIGGER fail_update BEFORE UPDATE ON jobs BEGIN SELECT RAISE(FAIL,'injected database failure'); END", "状态", false},
		{"logs", "CREATE TRIGGER fail_log BEFORE INSERT ON job_logs BEGIN SELECT RAISE(FAIL,'injected log failure'); END", "日志", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s, err := store.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			task := Task{ID: store.ID(), Name: "persistence failure", CreatedAt: store.Now()}
			job := Job{ID: store.ID(), TaskID: task.ID, Name: "fixture.sz", CreatedAt: task.CreatedAt, Status: "queued"}
			task.JobIDs = []string{job.ID}
			if _, err = s.DB.Exec("INSERT INTO tasks VALUES(?,?)", task.ID, store.Encode(task)); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec("INSERT INTO jobs VALUES(?,?,?)", job.ID, task.ID, store.Encode(job)); err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec(tc.trigger); err != nil {
				t.Fatal(err)
			}
			m := &Manager{store: s, Dir: filepath.Join(root, "decrypt"), jobs: map[string]Job{job.ID: job}, tasks: map[string]Task{task.ID: task}, ctx: context.Background()}
			if err = os.MkdirAll(m.Path(job.ID, ""), 0700); err != nil {
				t.Fatal(err)
			}
			uploadDir := filepath.Join(root, "uploads", store.ID())
			if err = os.MkdirAll(uploadDir, 0700); err != nil {
				t.Fatal(err)
			}
			video, err := os.ReadFile("../engine/testdata/encrypted.sz")
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(uploadDir, "fixture.sz")
			if err = os.WriteFile(input, video, 0600); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile("../engine/testdata/key.json")
			if err != nil {
				t.Fatal(err)
			}
			key, err := engine.ReadKey(raw)
			if err != nil {
				t.Fatal(err)
			}
			m.run(work{job.ID, Prepared{Input: input, Name: job.Name, UploadID: filepath.Base(uploadDir), Key: key}})
			got, _ := m.Job(job.ID)
			if got.Status != "failed" || !strings.Contains(got.Error, tc.want) || got.FinishedAt == "" || got.Result != nil {
				t.Fatalf("job must terminate with the real persistence error: %+v", got)
			}
			if _, err = os.Stat(uploadDir); !os.IsNotExist(err) {
				t.Fatal("owned upload was not cleaned", err)
			}
			if _, err = os.Stat(m.Path(job.ID, "output.mp4")); !os.IsNotExist(err) {
				t.Fatal("unpublished output survived", err)
			}
			if tc.durable {
				var payload string
				if err = s.DB.QueryRow("SELECT payload FROM jobs WHERE id=?", job.ID).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				var saved Job
				if err = json.Unmarshal([]byte(payload), &saved); err != nil || saved.Status != "failed" {
					t.Fatal("failed state not persisted", err, payload)
				}
			}
		})
	}
}

func TestProgressJournalKeepsMilestonesWithoutDuplicatingCompletion(t *testing.T) {
	var messages []string
	l := progressJournal{write: func(_, message string) { messages = append(messages, message) }}
	for i := 1; i <= 100; i++ {
		l.update(engine.Progress{Stage: "validate", Done: float64(i) / 50, Total: 2, Elapsed: float64(i) / 10})
	}
	l.update(engine.Progress{Stage: "validate", Done: 2, Total: 2, Elapsed: 10})
	if len(messages) > 12 || len(messages) < 10 || !strings.Contains(messages[len(messages)-1], "100%") {
		t.Fatalf("unexpected progress journal: %v", messages)
	}
	l.update(engine.Progress{Stage: "scan", Done: 1, Total: 1000, Elapsed: 11})
	if !strings.Contains(messages[len(messages)-1], "音频检查") {
		t.Fatal("new stage was not recorded")
	}
}

func TestFailureDoesNotBlockNextVideo(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	bad := filepath.Join(root, "broken.sz")
	os.WriteFile(bad, []byte("broken video"), 0600)
	raw, _ := os.ReadFile("../engine/testdata/key.json")
	k, _ := engine.ReadKey(raw)
	v, err := m.Create("mixed", []Prepared{{Input: bad, Name: "bad", Key: k}, {Input: "../engine/testdata/encrypted.sz", Name: "good", Key: k}})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(20 * time.Second)
	for {
		views, _ := m.List()
		current := views[0]
		if current.Status == "partial_failed" {
			if current.Completed != 1 || current.Failed != 1 {
				t.Fatal(current)
			}
			break
		}
		if time.Now().After(end) {
			t.Fatalf("task not finished: %s", current.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
	m.Close()
	m2, err := New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	views, _ := m2.List()
	if views[0].ID != v.ID || views[0].Status != "partial_failed" {
		t.Fatal("restart lost task results")
	}
}
func TestInterruptedRecoveryAndAtomicDelete(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task := Task{ID: store.ID(), Name: "interrupted", CreatedAt: store.Now()}
	job := Job{ID: store.ID(), TaskID: task.ID, Name: "partial", CreatedAt: task.CreatedAt, Status: "running"}
	task.JobIDs = []string{job.ID}
	if _, err = s.DB.Exec("INSERT INTO tasks VALUES(?,?)", task.ID, store.Encode(task)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO jobs VALUES(?,?,?)", job.ID, task.ID, store.Encode(job)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "decrypt", job.ID)
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, ".partial.mp4"), []byte("partial"), 0600)
	os.WriteFile(filepath.Join(dir, "output.mp4"), []byte("unpublished"), 0600)
	m, err := New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	j, _ := m.Job(job.ID)
	if j.Status != "failed" || j.Error == "" {
		t.Fatal("running job not recovered as interrupted")
	}
	if _, err = os.Stat(filepath.Join(dir, ".partial.mp4")); !os.IsNotExist(err) {
		t.Fatal("partial result not cleaned")
	}
	if _, err = os.Stat(filepath.Join(dir, "output.mp4")); !os.IsNotExist(err) {
		t.Fatal("interrupted output was kept despite never being published")
	}
	if _, err = m.Delete([]string{task.ID, "unknown"}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if _, ok := m.Job(job.ID); !ok {
		t.Fatal("partial batch deletion occurred")
	}
	if _, err = m.Delete([]string{task.ID}); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM job_logs").Scan(&n)
	if n != 0 {
		t.Fatal("logs did not cascade")
	}
}

func TestStartupDoesNotFollowJobDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task := Task{ID: store.ID(), Name: "symlink", CreatedAt: store.Now()}
	job := Job{ID: store.ID(), TaskID: task.ID, Name: "fixture.sz", CreatedAt: task.CreatedAt, Status: "failed"}
	task.JobIDs = []string{job.ID}
	if _, err = s.DB.Exec("INSERT INTO tasks VALUES(?,?)", task.ID, store.Encode(task)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO jobs VALUES(?,?,?)", job.ID, task.ID, store.Encode(job)); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	important := filepath.Join(external, "original.sz")
	if err = os.WriteFile(important, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, "decrypt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(external, filepath.Join(root, "decrypt", job.ID)); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, root)
	if m != nil {
		m.Close()
	}
	if err == nil {
		t.Error("symlinked job directory accepted")
	}
	data, readErr := os.ReadFile(important)
	if readErr != nil || string(data) != "preserve" {
		t.Fatal("startup touched an external file", readErr)
	}
}

func TestActivityBracketsAQueueRun(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	events := make(chan bool, 8)
	m.SetActivity(func(active bool) { events <- active })
	finished := make(chan Finished, 2)
	m.SetTaskFinished(func(result Finished) { finished <- result })
	raw, _ := os.ReadFile("../engine/testdata/key.json")
	k, _ := engine.ReadKey(raw)
	bad := filepath.Join(root, "broken.sz")
	os.WriteFile(bad, []byte("broken video"), 0600)
	items := []Prepared{{Input: bad, Name: "bad", Key: k}, {Input: "../engine/testdata/encrypted.sz", Name: "good", Key: k}}
	task, err := m.Create("activity", items)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []bool{true, false} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("activity = %v, want %v", got, want)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("no activity event %v", want)
		}
	}
	select {
	case result := <-finished:
		if result.ID != task.ID || result.Status != "partial_failed" || result.Total != 2 || result.Completed != 1 || result.Failed != 1 {
			t.Fatalf("wrong final notification: %+v", result)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no final task notification")
	}
	select {
	case extra := <-events:
		t.Fatalf("unexpected extra activity event %v for a single drain", extra)
	case extra := <-finished:
		t.Fatalf("duplicate final task notification: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}
