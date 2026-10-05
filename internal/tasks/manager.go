package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/store"
)

type Job struct {
	ID         string           `json:"id"`
	TaskID     string           `json:"taskId"`
	Name       string           `json:"name"`
	CreatedAt  string           `json:"createdAt"`
	StartedAt  string           `json:"startedAt,omitempty"`
	FinishedAt string           `json:"finishedAt,omitempty"`
	Status     string           `json:"status"`
	Retryable  bool             `json:"retryable"`
	Attempt    int              `json:"attempt"`
	Progress   *engine.Progress `json:"progress,omitempty"`
	Error      string           `json:"error,omitempty"`
	Result     any              `json:"result,omitempty"`
}
type Task struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	JobIDs    []string `json:"jobIds"`
}
type View struct {
	Task
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	Children  []Job  `json:"children"`
}
type Finished struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}
type Prepared struct {
	Input, Name, UploadID, AppHash string
	Key                            engine.Key
}
type work struct {
	ID string
	Prepared
}
type Upload struct {
	Name, Path string
	At         time.Time
}
type Manager struct {
	mu      sync.Mutex
	store   *store.Store
	Dir     string
	jobs    map[string]Job
	tasks   map[string]Task
	uploads map[string]Upload
	queue   []work
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	closed  bool
	// activity reports whether the queue is draining, e.g. to hold off sleep.
	activity func(active bool)
	finished func(Finished)
}

var idPattern = regexp.MustCompile(`^[a-f0-9-]{32,36}$`)

func New(s *store.Store, dataDir string) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{store: s, Dir: filepath.Join(dataDir, "decrypt"), jobs: map[string]Job{}, tasks: map[string]Task{}, uploads: map[string]Upload{}, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		cancel()
		return nil, err
	}
	load := func(query string, read func([]byte) error) error {
		rows, e := s.DB.Query(query)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				return e
			}
			if e = read([]byte(raw)); e != nil {
				return e
			}
		}
		return rows.Err()
	}
	err := load("SELECT payload FROM tasks", func(b []byte) error {
		var t Task
		if e := json.Unmarshal(b, &t); e != nil {
			return e
		}
		if !idPattern.MatchString(t.ID) {
			return errors.New("任务 ID 无效")
		}
		m.tasks[t.ID] = t
		return nil
	})
	if err != nil {
		cancel()
		return nil, err
	}
	err = load("SELECT payload FROM jobs", func(b []byte) error {
		var j Job
		if e := json.Unmarshal(b, &j); e != nil {
			return e
		}
		if !idPattern.MatchString(j.ID) {
			return errors.New("子任务 ID 无效")
		}
		m.jobs[j.ID] = j
		return nil
	})
	if err != nil {
		cancel()
		return nil, err
	}
	for _, j := range m.jobs {
		if active(j.Status) {
			j.Status = "failed"
			j.Error = "服务重启导致任务中断，可重试解密"
			j.FinishedAt = store.Now()
			if err = m.save(j); err != nil {
				cancel()
				return nil, err
			}
			if err = s.AppendJobLog(j.ID, "ERROR", j.Error); err != nil {
				cancel()
				return nil, err
			}
		}
		jobDir := filepath.Join(m.Dir, j.ID)
		info, e := os.Lstat(jobDir)
		if e != nil && !os.IsNotExist(e) {
			cancel()
			return nil, e
		}
		if e == nil && !info.IsDir() {
			cancel()
			return nil, fmt.Errorf("子任务工作目录无效，不能使用文件或符号链接：%s", j.ID)
		}
		entries, e := os.ReadDir(jobDir)
		if e != nil && !os.IsNotExist(e) {
			cancel()
			return nil, e
		}
		for _, entry := range entries {
			keep := entry.Type().IsRegular() && (entry.Name() == "input.sz" || entry.Name() == "request.json" || entry.Name() == "output.mp4" && j.Status == "completed")
			if !keep {
				if e = os.RemoveAll(filepath.Join(m.Dir, j.ID, entry.Name())); e != nil {
					cancel()
					return nil, e
				}
			}
		}
	}
	for id, j := range m.jobs {
		_, err := m.readPrepared(id)
		j.Retryable = err == nil
		m.jobs[id] = j
	}
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, e := range entries {
		if idPattern.MatchString(e.Name()) {
			if _, ok := m.jobs[e.Name()]; !ok {
				if err = os.RemoveAll(filepath.Join(m.Dir, e.Name())); err != nil {
					cancel()
					return nil, err
				}
			}
		}
	}
	if err = m.cleanup(); err != nil {
		cancel()
		return nil, err
	}
	for _, j := range m.jobs {
		if j.Status == "completed" {
			if err := m.publishResource(j); err != nil && !os.IsNotExist(err) {
				cancel()
				return nil, fmt.Errorf("导入已有视频资源失败：%w", err)
			}
		}
	}
	go m.loop()
	return m, nil
}
func (m *Manager) publishResource(j Job) error {
	finished := j.FinishedAt
	if finished == "" {
		finished = j.CreatedAt
	}
	return m.store.PublishResource(filepath.Dir(m.Dir), m.Path(j.ID, "output.mp4"), store.Resource{ID: j.ID, Name: strings.TrimSuffix(j.Name, filepath.Ext(j.Name)) + ".mp4", TaskID: j.TaskID, TaskName: m.tasks[j.TaskID].Name, FinishedAt: finished})
}
func active(s string) bool { return s == "queued" || s == "running" }
func (m *Manager) save(j Job) error {
	_, err := m.store.DB.Exec("UPDATE jobs SET payload=? WHERE id=?", store.Encode(j), j.ID)
	if err == nil {
		m.jobs[j.ID] = j
	}
	return err
}

// Called with m.mu held. Never leave a finished worker looking active when its
// final database write fails. Try to persist the failure; a full storage outage
// still leaves an honest in-memory state, with restart recovery as the fallback.
func (m *Manager) saveFinal(j Job) Job {
	if err := m.save(j); err != nil {
		j.Status, j.Result = "failed", nil
		if j.Error != "" {
			j.Error += "；"
		}
		j.Error += "最终任务状态写入失败：" + err.Error()
		slog.Error("最终任务状态写入失败", "job_id", j.ID, "error", err)
		if logErr := m.store.AppendJobLog(j.ID, "ERROR", j.Error); logErr != nil {
			slog.Error("任务失败日志写入失败", "job_id", j.ID, "error", logErr)
		}
		if retryErr := m.save(j); retryErr != nil {
			m.jobs[j.ID] = j
			slog.Error("失败状态无法持久化，已停止当前任务", "job_id", j.ID, "error", retryErr)
		}
	}
	return j
}
func (m *Manager) view(t Task) View {
	v := View{Task: t, Children: make([]Job, 0, len(t.JobIDs)), Status: "completed", Total: len(t.JobIDs)}
	running, queued := false, false
	for _, id := range t.JobIDs {
		j := m.jobs[id]
		v.Children = append(v.Children, j)
		switch j.Status {
		case "running":
			running = true
		case "queued":
			queued = true
		case "completed":
			v.Completed++
		case "failed":
			v.Failed++
		}
	}
	if running {
		v.Status = "running"
	} else if queued {
		v.Status = "queued"
	} else if v.Failed == v.Total {
		v.Status = "failed"
	} else if v.Failed > 0 {
		v.Status = "partial_failed"
	}
	return v
}
func (m *Manager) List() ([]View, []Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := make([]View, 0, len(m.tasks))
	jobs := make([]Job, 0, len(m.jobs))
	for _, t := range m.tasks {
		views = append(views, m.view(t))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt > views[j].CreatedAt })
	for _, v := range views {
		jobs = append(jobs, v.Children...)
	}
	return views, jobs
}
func (m *Manager) Job(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}
func (m *Manager) Path(id, name string) string { return filepath.Join(m.Dir, id, name) }
func (m *Manager) AddUpload(id, name, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("服务正在关闭")
	}
	m.uploads[id] = Upload{name, path, time.Now()}
	return nil
}
func (m *Manager) Upload(id string) (Upload, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.uploads[id]
	return v, ok && time.Since(v.At) < 24*time.Hour
}
func (m *Manager) Create(name string, items []Prepared) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return View{}, errors.New("服务正在关闭")
	}
	if len(m.queue)+len(items) > 1000 {
		return View{}, errors.New("排队视频已达 1000 个，请等待任务完成")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 || len(items) < 1 || len(items) > 100 {
		return View{}, errors.New("任务名称须为 1–100 字，视频须为 1–100 个")
	}
	seen := map[string]bool{}
	for _, v := range items {
		if seen[v.Input] {
			return View{}, errors.New("同一任务不能重复选择视频")
		}
		seen[v.Input] = true
		if v.UploadID != "" {
			if _, ok := m.uploads[v.UploadID]; !ok {
				return View{}, errors.New("上传文件已被使用")
			}
		}
	}
	t := Task{ID: store.ID(), Name: name, CreatedAt: store.Now(), JobIDs: make([]string, 0, len(items))}
	children := make([]Job, 0, len(items))
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, p := range created {
				os.RemoveAll(p)
			}
		}
	}()
	for i, v := range items {
		j := Job{ID: store.ID(), TaskID: t.ID, Name: v.Name, CreatedAt: t.CreatedAt, Status: "queued", Retryable: true, Attempt: 1}
		if err := os.Mkdir(m.Path(j.ID, ""), 0700); err != nil {
			return View{}, err
		}
		created = append(created, m.Path(j.ID, ""))
		prepared, err := m.retainInput(j.ID, v)
		if err != nil {
			return View{}, err
		}
		items[i] = prepared
		children = append(children, j)
		t.JobIDs = append(t.JobIDs, j.ID)
	}
	tx, err := m.store.DB.Begin()
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO tasks VALUES(?,?)", t.ID, store.Encode(t)); err != nil {
		return View{}, err
	}
	for _, j := range children {
		if _, err = tx.Exec("INSERT INTO jobs VALUES(?,?,?)", j.ID, t.ID, store.Encode(j)); err != nil {
			return View{}, err
		}
		b := []byte(fmt.Sprintf("[%s] [INFO] 子任务已创建，等待执行。任务 %s；视频 %s\n", time.Now().Format("2006-01-02 15:04:05"), t.ID, j.Name))
		if _, err = tx.Exec("INSERT INTO job_logs VALUES(?,0,?)", j.ID, b); err != nil {
			return View{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return View{}, err
	}
	committed = true
	m.tasks[t.ID] = t
	for i, j := range children {
		m.jobs[j.ID] = j
		m.queue = append(m.queue, work{j.ID, items[i]})
		if u, ok := m.uploads[items[i].UploadID]; ok {
			delete(m.uploads, items[i].UploadID)
			if err := os.RemoveAll(filepath.Dir(u.Path)); err != nil {
				slog.Warn("已接收上传文件等待清理", "error", err)
			}
		}
		m.queue[len(m.queue)-1].UploadID = ""
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return m.view(t), nil
}
func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			m.failQueued()
			return
		case <-ticker.C:
			m.expireUploads()
		case <-m.wake:
		}
		busy := false
		for {
			m.mu.Lock()
			if len(m.queue) == 0 || m.closed {
				m.mu.Unlock()
				break
			}
			w := m.queue[0]
			m.queue = m.queue[1:]
			m.mu.Unlock()
			if !busy {
				busy = true
				m.setActive(true)
			}
			m.run(w)
		}
		if busy {
			m.setActive(false)
		}
	}
}
func (m *Manager) run(w work) {
	ctx, cancel := context.WithCancelCause(m.ctx)
	defer cancel(nil)
	log := func(level, message string) {
		if e := m.store.AppendJobLog(w.ID, level, message); e != nil {
			cancel(fmt.Errorf("日志持久化失败：%w", e))
			slog.Error("子任务日志写入失败", "job_id", w.ID, "error", e)
		}
	}
	m.mu.Lock()
	j := m.jobs[w.ID]
	j.Status = "running"
	j.StartedAt = store.Now()
	err := m.save(j)
	m.mu.Unlock()
	if err != nil {
		err = fmt.Errorf("任务状态写入失败：%w", err)
	}
	var result map[string]any
	if err == nil {
		slog.Info("开始解密", "task_id", j.TaskID, "job_id", j.ID, "video", j.Name)
		m.systemLog("info", "开始解密："+j.Name, j.ID)
		log("INFO", fmt.Sprintf("开始处理：%s\n任务编号：%s\n子任务编号：%s", j.Name, j.TaskID, j.ID))
		progressLog := progressJournal{write: log}
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("解密引擎异常：%v", r)
					log("ERROR", string(debug.Stack()))
				}
			}()
			result, err = engine.Run(ctx, w.Input, m.Path(w.ID, "output.mp4"), w.Key, w.AppHash, log, func(p engine.Progress) {
				m.mu.Lock()
				current := m.jobs[w.ID]
				current.Progress = &p
				e := m.save(current)
				m.mu.Unlock()
				if e != nil {
					cancel(fmt.Errorf("任务进度写入失败：%w", e))
					log("ERROR", "任务进度写入失败："+e.Error())
					return
				}
				progressLog.update(p)
			})
		}()
	}
	if cause := context.Cause(ctx); cause != nil {
		err = cause
	}
	m.mu.Lock()
	j = m.jobs[w.ID]
	m.mu.Unlock()
	j.FinishedAt = store.Now()
	if err != nil {
		j.Status = "failed"
		j.Error = err.Error()
		if errors.Is(err, context.Canceled) {
			j.Error = "服务停止，当前视频处理已中断"
		}
		log("ERROR", fmt.Sprintf("处理失败：%s\n处理结果：未生成可下载文件。", j.Error))
	} else {
		j.Status = "completed"
		j.Result = result
		log("INFO", fmt.Sprintf("视频处理结束 · 总耗时 %.1f 秒 · MP4 校验通过", result["elapsed_sec"]))
	}
	if w.UploadID != "" {
		if e := os.RemoveAll(filepath.Dir(w.Input)); e != nil {
			log("WARN", "上传文件清理失败："+e.Error())
		}
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		j.Status = "failed"
		j.Error = cause.Error()
	}
	if j.Status == "failed" {
		j.Result = nil
	}
	m.mu.Lock()
	if j.Status == "completed" {
		if e := m.publishResource(j); e != nil {
			j.Status = "failed"
			j.Error = "视频入库失败：" + e.Error()
			j.Result = nil
			log("ERROR", j.Error)
		}
	}
	j = m.saveFinal(j)
	view := m.view(m.tasks[j.TaskID])
	finished := m.finished
	m.mu.Unlock()
	level := "info"
	if j.Status == "failed" {
		level = "error"
		os.Remove(m.Path(j.ID, "output.mp4"))
	}
	label := "解密完成"
	if j.Status == "failed" {
		label = "解密失败"
	}
	message := label + "：" + j.Name
	if j.Error != "" {
		message += " · " + j.Error
	}
	m.systemLog(level, message, j.ID)
	if finished != nil && view.Total > 0 && !active(view.Status) && (m.ctx == nil || m.ctx.Err() == nil) {
		finished(Finished{ID: view.ID, Status: view.Status, Total: view.Total, Completed: view.Completed, Failed: view.Failed})
	}
}

// UI progress still persists every update; the journal records milestones and
// a heartbeat for long stages instead of repeating a line every half second.
type progressJournal struct {
	write   engine.LogFunc
	stage   string
	percent int
	elapsed float64
}

func (l *progressJournal) update(p engine.Progress) {
	if p.Stage == "complete" || p.Total <= 0 || p.Done <= 0 {
		return
	}
	percent := int(math.Min(100, math.Max(0, p.Done/p.Total*100)))
	if p.Stage == l.stage && (percent == l.percent || percent < l.percent+10 && p.Elapsed-l.elapsed < 5 && percent < 100) {
		return
	}
	l.stage, l.percent, l.elapsed = p.Stage, percent, p.Elapsed
	l.write("PROGRESS", fmt.Sprintf("%s进度：%d%% · 阶段耗时 %.1f 秒 · 总耗时 %.1f 秒", engine.StageName(p.Stage), percent, p.StageElapsed, p.Elapsed))
}
func (m *Manager) systemLog(level, message, id string) {
	if e := m.store.Log(level, "decrypt", message, "", id); e != nil {
		slog.Error("系统日志写入失败", "error", e)
	}
}
func (m *Manager) failQueued() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.queue {
		j := m.jobs[w.ID]
		j.Status = "failed"
		j.Error = "服务已关闭，任务尚未执行"
		j.FinishedAt = store.Now()
		j = m.saveFinal(j)
		m.store.AppendJobLog(j.ID, "ERROR", j.Error)
		if w.UploadID != "" {
			os.RemoveAll(filepath.Dir(w.Input))
		}
	}
	m.queue = nil
}

// SetActivity registers a callback told when the queue starts and stops running jobs.
func (m *Manager) SetActivity(f func(active bool)) {
	m.mu.Lock()
	m.activity = f
	m.mu.Unlock()
}
func (m *Manager) SetTaskFinished(f func(Finished)) {
	m.mu.Lock()
	m.finished = f
	m.mu.Unlock()
}
func (m *Manager) setActive(active bool) {
	m.mu.Lock()
	f := m.activity
	m.mu.Unlock()
	if f != nil {
		f(active)
	}
}
func (m *Manager) Close() {
	// Input retention holds mu. Signal cancellation first so it can stop copying
	// a large file and release the lock instead of making shutdown wait for it.
	m.cancel()
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	<-m.done
}

type DeleteResult struct {
	Deleted        []string `json:"deleted"`
	CleanupPending bool     `json:"cleanupPending"`
}

func (m *Manager) Delete(ids []string) (DeleteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := DeleteResult{Deleted: make([]string, 0, len(ids))}
	if len(ids) < 1 || len(ids) > 100 {
		return DeleteResult{}, errors.New("请选择 1–100 个任务")
	}
	unique := map[string]bool{}
	for _, id := range ids {
		t, ok := m.tasks[id]
		if !ok {
			return DeleteResult{}, errors.New("任务不存在")
		}
		if !unique[id] {
			result.Deleted = append(result.Deleted, id)
		}
		unique[id] = true
		for _, jid := range t.JobIDs {
			if active(m.jobs[jid].Status) {
				return DeleteResult{}, errors.New("不能删除进行中或排队中的任务")
			}
		}
	}
	tx, err := m.store.DB.Begin()
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback()
	for id := range unique {
		for _, jid := range m.tasks[id].JobIDs {
			if _, err = tx.Exec("INSERT OR IGNORE INTO file_cleanup VALUES(?)", jid); err != nil {
				return DeleteResult{}, err
			}
		}
		if _, err = tx.Exec("DELETE FROM tasks WHERE id=?", id); err != nil {
			return DeleteResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return DeleteResult{}, err
	}
	for id := range unique {
		for _, jid := range m.tasks[id].JobIDs {
			delete(m.jobs, jid)
		}
		delete(m.tasks, id)
	}
	if err = m.cleanup(); err != nil {
		result.CleanupPending = true
		slog.Error("任务文件等待清理", "error", err)
	}
	return result, nil
}
func (m *Manager) cleanup() error {
	rows, err := m.store.DB.Query("SELECT job_id FROM file_cleanup")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			return errors.New("待清理任务 ID 无效")
		}
		if err = os.RemoveAll(m.Path(id, "")); err != nil {
			return err
		}
		if _, err = m.store.DB.Exec("DELETE FROM file_cleanup WHERE job_id=?", id); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) expireUploads() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, u := range m.uploads {
		if time.Since(u.At) > 24*time.Hour {
			if err := os.RemoveAll(filepath.Dir(u.Path)); err == nil {
				delete(m.uploads, id)
			}
		}
	}
	if err := m.cleanup(); err != nil {
		slog.Error("任务文件清理失败", "error", err)
	}
}
