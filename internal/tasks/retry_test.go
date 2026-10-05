package tasks

import (
	"os"
	"path/filepath"
	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/store"
	"testing"
	"time"
)

func waitTerminal(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Job(id)
		if !active(j.Status) {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func TestRetrySurvivesRestartAndPreservesResources(t *testing.T) {
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
	defer func() { m.Close() }()
	raw, _ := os.ReadFile("../engine/testdata/key.json")
	key, err := engine.ReadKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec("CREATE TRIGGER fail_start BEFORE UPDATE ON jobs WHEN json_extract(NEW.payload,'$.status')='running' BEGIN SELECT RAISE(FAIL,'temporary fault'); END")
	if err != nil {
		t.Fatal(err)
	}
	v, err := m.Create("retry", []Prepared{{Input: "../engine/testdata/encrypted.sz", Name: "video.sz", Key: key}})
	if err != nil {
		t.Fatal(err)
	}
	j := waitTerminal(t, m, v.Children[0].ID)
	if j.Status != "failed" {
		t.Fatal(j)
	}
	m.Close()
	s.DB.Exec("DROP TRIGGER fail_start")
	m, err = New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	j, _ = m.Job(j.ID)
	if !j.Retryable {
		t.Fatal("restart removed retry input")
	}
	// A rejected mixed request cannot partially queue a job.
	if _, err = m.Retry(v.ID, []string{j.ID, "unknown"}); err == nil {
		t.Fatal("accepted invalid batch")
	}
	j, _ = m.Job(j.ID)
	if j.Status != "failed" {
		t.Fatal("partially queued invalid batch")
	}
	// Log persistence belongs to the same transaction as retry status.
	s.DB.Exec("CREATE TRIGGER fail_retry_log BEFORE INSERT ON job_logs BEGIN SELECT RAISE(FAIL,'log fault'); END")
	if _, err = m.Retry(v.ID, []string{j.ID}); err == nil {
		t.Fatal("ignored retry log failure")
	}
	j, _ = m.Job(j.ID)
	if j.Status != "failed" {
		t.Fatal("retry state escaped rollback")
	}
	s.DB.Exec("DROP TRIGGER fail_retry_log")
	if _, err = m.Retry(v.ID, []string{j.ID}); err != nil {
		t.Fatal(err)
	}
	j = waitTerminal(t, m, j.ID)
	if j.Status != "completed" || j.Attempt != 2 {
		t.Fatal(j)
	}
	requestPath := m.Path(j.ID, "request.json")
	if info, err := os.Stat(requestPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("retry key permissions", err)
	}
	if _, err = m.Delete([]string{v.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Dir(requestPath)); !os.IsNotExist(err) {
		t.Fatal("deleted task retained retry files")
	}
	if _, err = os.Stat(store.ResourcePath(root, j.ID)); err != nil {
		t.Fatal("task deletion lost output", err)
	}
}
