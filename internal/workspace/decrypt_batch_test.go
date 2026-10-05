package workspace

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/tasks"
)

func waitTask(t *testing.T, app *App, id string) tasks.View {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := app.Tasks.Task(id)
		if ok && view.Status != "queued" && view.Status != "running" {
			return view
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task never finished")
	return tasks.View{}
}

func TestBatchEveryVideoRetryAndPrivateList(t *testing.T) {
	app := testApp(t)
	source, err := os.ReadFile("../engine/testdata/encrypted.sz")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("../engine/testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	videos := make([]TaskVideoInput, 0, 3)
	for index := range 3 {
		body := source
		if index == 1 {
			body = []byte("broken")
		}
		choice := videoChoice(t, app, "same.sz", body)
		imported, err := app.ImportVideo(context.Background(), choice, nil)
		if err != nil {
			t.Fatal(err)
		}
		videos = append(videos, TaskVideoInput{UploadID: imported.ID, KeyJSON: string(key)})
	}
	task, err := app.CreateTask(CreateTaskInput{Name: "多任务 2026-09-25 13:00:00", Videos: videos})
	if err != nil {
		t.Fatal(err)
	}
	task = waitTask(t, app, task.ID)
	if task.Total != 3 || task.Completed != 2 || task.Failed != 1 {
		t.Fatalf("lost batch video: %+v", task)
	}
	var resources int
	if err := app.Store.DB.QueryRow("SELECT count(*) FROM resources WHERE task_id=?", task.ID).Scan(&resources); err != nil || resources != 2 {
		t.Fatalf("resources = %d, %v", resources, err)
	}
	failed := task.Children[1]
	if !failed.Retryable {
		t.Fatal("failure lost retry input")
	}
	for _, job := range task.Children {
		if _, err := os.Stat(app.Tasks.Path(job.ID, "input.sz")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(app.Tasks.Path(failed.ID, "input.sz"), source, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RetryTask(task.ID, []string{failed.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RetryTask(task.ID, []string{failed.ID}); err == nil {
		t.Fatal("duplicate retry accepted")
	}
	task = waitTask(t, app, task.ID)
	if task.Completed != 3 || task.Children[1].Attempt != 2 {
		t.Fatalf("retry failed: %+v", task)
	}
	if task.Children[0].Attempt != 1 || task.Children[2].Attempt != 1 {
		t.Fatal("successful siblings were rerun")
	}
	listed, err := app.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(listed)
	if err != nil || strings.Contains(string(encoded), `"getPwdData":`) || strings.Contains(string(encoded), `"passwords":`) {
		t.Fatal("private retry recipe leaked")
	}
}
