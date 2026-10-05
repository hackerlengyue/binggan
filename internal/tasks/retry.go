package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"time.haomen/binggan/v2/internal/store"
)

// Every job owns its input snapshot. Retrying must survive upload cleanup and
// application restarts, without retaining paths into another job's directory.
func (m *Manager) retainInput(id string, p Prepared) (Prepared, error) {
	if err := m.ctx.Err(); err != nil {
		return p, err
	}
	source, err := os.Open(p.Input)
	if err != nil {
		return p, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return p, err
	}
	if !info.Mode().IsRegular() {
		return p, errors.New("源视频不是普通文件")
	}
	dest, err := os.OpenFile(m.Path(id, "input.sz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return p, err
	}
	_, err = io.Copy(dest, inputReader{ctx: m.ctx, source: source})
	if err == nil {
		err = m.ctx.Err()
	}
	if err == nil {
		err = dest.Sync()
	}
	closeErr := dest.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return p, err
	}
	p.Input = m.Path(id, "input.sz")
	saved := p
	saved.UploadID = ""
	data, err := json.Marshal(saved)
	if err != nil {
		return p, err
	}
	err = os.WriteFile(m.Path(id, "request.json"), data, 0600)
	return p, err
}

type inputReader struct {
	ctx    context.Context
	source io.Reader
}

func (r inputReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
func (m *Manager) readPrepared(id string) (Prepared, error) {
	var p Prepared
	for _, name := range []string{"input.sz", "request.json"} {
		info, err := os.Lstat(m.Path(id, name))
		if err != nil || !info.Mode().IsRegular() {
			return p, errors.New("此任务未保留源文件，请重新选择视频")
		}
	}
	raw, err := os.ReadFile(m.Path(id, "request.json"))
	if err != nil {
		return p, err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&p); err != nil {
		return p, err
	}
	if err = p.Key.Validate(); err != nil {
		return p, err
	}
	p.Input = m.Path(id, "input.sz")
	p.UploadID = ""
	return p, nil
}

// All requested failures are queued atomically. Successful siblings and their
// resource files remain untouched; simultaneous clicks cannot enqueue twice.
func (m *Manager) Retry(taskID string, ids []string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return View{}, errors.New("任务不存在")
	}
	if m.closed {
		return View{}, errors.New("服务正在关闭")
	}
	if len(ids) < 1 || len(ids) > 100 {
		return View{}, errors.New("请选择要重试的视频")
	}
	pending := []work{}
	jobs := []Job{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		j, ok := m.jobs[id]
		if !ok || j.TaskID != taskID {
			return View{}, errors.New("视频不属于此任务")
		}
		if j.Status != "failed" {
			return View{}, errors.New("仅失败的视频可以重试，请勿重复提交")
		}
		p, err := m.readPrepared(id)
		if err != nil {
			return View{}, err
		}
		j.Status = "queued"
		j.Error = ""
		j.Progress = nil
		j.Result = nil
		j.StartedAt = ""
		j.FinishedAt = ""
		j.Retryable = true
		j.Attempt = max(1, j.Attempt) + 1
		pending = append(pending, work{id, p})
		jobs = append(jobs, j)
	}
	if len(m.queue)+len(pending) > 1000 {
		return View{}, errors.New("排队视频已达 1000 个，请稍后重试")
	}
	tx, err := m.store.DB.Begin()
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	for _, j := range jobs {
		if _, err = tx.Exec("UPDATE jobs SET payload=? WHERE id=?", store.Encode(j), j.ID); err != nil {
			return View{}, err
		}
		line := []byte(fmt.Sprintf("[%s] [INFO] 第 %d 次尝试 · 已重新排队\n", time.Now().Format("2006-01-02 15:04:05"), j.Attempt))
		if _, err = tx.Exec("INSERT INTO job_logs(job_id,byte_start,data) SELECT ?,COALESCE(MAX(byte_start+length(data)),0),? FROM job_logs WHERE job_id=?", j.ID, line, j.ID); err != nil {
			return View{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return View{}, err
	}
	for _, j := range jobs {
		m.jobs[j.ID] = j
	}
	m.queue = append(m.queue, pending...)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return m.view(t), nil
}
func (m *Manager) Task(id string) (View, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return View{}, false
	}
	return m.view(t), true
}
