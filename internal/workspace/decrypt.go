package workspace

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/tasks"
)

type FileChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func listFiles(root, ext string) ([]FileChoice, error) {
	out := make([]FileChoice, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ext) {
			id, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			out = append(out, FileChoice{id, id})
		}
		return nil
	})
	return out, err
}
func safeFile(root, id, ext string) (string, error) {
	if id == "" || !filepath.IsLocal(id) || !strings.EqualFold(filepath.Ext(id), ext) {
		return "", errors.New("文件不存在")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", errors.New("文件不存在")
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("不允许使用符号链接")
	}
	if !rootInfo.IsDir() {
		return "", errors.New("文件不存在")
	}
	cur := root
	for _, part := range strings.Split(filepath.Clean(id), string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return "", errors.New("文件不存在")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("不允许使用符号链接")
		}
	}
	info, err := os.Stat(cur)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("请选择普通文件")
	}
	return cur, nil
}

type TaskList struct {
	Files []FileChoice `json:"files"`
	Keys  []FileChoice `json:"keys"`
	Tasks []tasks.View `json:"tasks"`
	Jobs  []tasks.Job  `json:"jobs"`
}

func (a *App) ListTasks() (TaskList, error) {
	files, err := listFiles(filepath.Join(a.Config.Root, "input"), ".sz")
	if err != nil {
		return TaskList{}, err
	}
	keys, err := listFiles(filepath.Join(a.Config.Root, "keys"), ".json")
	if err != nil {
		return TaskList{}, err
	}
	views, jobs := a.Tasks.List()
	return TaskList{files, keys, views, jobs}, nil
}

type TaskCounts struct {
	Total     int `json:"total"`
	Active    int `json:"active"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}
type TaskSummary struct {
	Tasks  TaskCounts `json:"tasks"`
	Videos TaskCounts `json:"videos"`
}

func (a *App) TaskSummary() TaskSummary {
	views, jobs := a.Tasks.List()
	t := TaskCounts{Total: len(views)}
	v := TaskCounts{Total: len(jobs)}
	add := func(m *TaskCounts, s string) {
		switch s {
		case "queued", "running":
			m.Active++
		case "completed":
			m.Completed++
		default:
			m.Failed++
		}
	}
	for _, item := range views {
		add(&t, item.Status)
	}
	for _, item := range jobs {
		add(&v, item.Status)
	}
	return TaskSummary{t, v}
}

type TaskVideoInput struct {
	SourceID string `json:"sourceId"`
	UploadID string `json:"uploadId"`
	KeyID    string `json:"keyId"`
	KeyJSON  string `json:"keyJson"`
}

type CreateTaskInput struct {
	Name   string           `json:"name"`
	Videos []TaskVideoInput `json:"videos"`
}

type taskInputError struct{ message string }

func (e *taskInputError) Error() string { return e.message }
func invalidTask(message string) error  { return &taskInputError{message} }

var errTaskConflict = errors.New("任务冲突")

type taskConflictError struct{ cause error }

func (e *taskConflictError) Error() string { return e.cause.Error() }
func (e *taskConflictError) Unwrap() error { return errTaskConflict }

func (a *App) CreateTask(v CreateTaskInput) (tasks.View, error) {
	if strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 100 || len(v.Videos) < 1 || len(v.Videos) > 100 {
		return tasks.View{}, invalidTask("任务名称须为 1–100 字，视频须为 1–100 个")
	}
	items := make([]tasks.Prepared, 0, len(v.Videos))
	totalKeyBytes := 0
	for _, video := range v.Videos {
		totalKeyBytes += len(video.KeyJSON)
		if totalKeyBytes > 2<<20 {
			return tasks.View{}, invalidTask("密钥文件过大")
		}
		var p tasks.Prepared
		var err error
		if video.UploadID != "" && video.SourceID != "" {
			return tasks.View{}, invalidTask("每个视频只能选择一种来源")
		}
		if video.UploadID != "" {
			u, ok := a.Tasks.Upload(video.UploadID)
			if !ok {
				return tasks.View{}, invalidTask("上传文件不存在或已使用")
			}
			p.Input = u.Path
			p.Name = u.Name
			p.UploadID = video.UploadID
		} else {
			p.Input, err = safeFile(filepath.Join(a.Config.Root, "input"), video.SourceID, ".sz")
			if err != nil {
				return tasks.View{}, invalidTask(err.Error())
			}
			p.Name = filepath.Base(p.Input)
		}
		raw := []byte(video.KeyJSON)
		if video.KeyID != "" {
			path, e := safeFile(filepath.Join(a.Config.Root, "keys"), video.KeyID, ".json")
			if e != nil {
				return tasks.View{}, invalidTask(e.Error())
			}
			file, e := os.Open(path)
			if e != nil {
				return tasks.View{}, e
			}
			raw, e = io.ReadAll(io.LimitReader(file, (2<<20)+1))
			file.Close()
			if e != nil {
				return tasks.View{}, e
			}
			if len(raw) > 2<<20 {
				return tasks.View{}, invalidTask("密钥文件过大")
			}
		}
		p.Key, err = engine.ReadKey(raw)
		if err != nil {
			return tasks.View{}, invalidTask(p.Name + "：" + err.Error())
		}
		p.AppHash, err = a.keyHash(p.Key)
		if err != nil {
			return tasks.View{}, invalidTask(p.Name + "：" + err.Error())
		}
		items = append(items, p)
	}
	result, err := a.Tasks.Create(v.Name, items)
	if err != nil {
		return tasks.View{}, &taskConflictError{err}
	}
	return result, nil
}
func (a *App) DeleteTasks(ids []string) (tasks.DeleteResult, error) {
	if len(ids) < 1 || len(ids) > 100 {
		return tasks.DeleteResult{}, invalidTask("请选择 1–100 个任务")
	}
	for _, id := range ids {
		if id == "" {
			return tasks.DeleteResult{}, invalidTask("任务 ID 不能为空")
		}
	}
	return a.Tasks.Delete(ids)
}
