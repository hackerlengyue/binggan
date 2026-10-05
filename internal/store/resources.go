package store

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

type Resource struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TaskID     string `json:"taskId"`
	TaskName   string `json:"taskName"`
	Size       int64  `json:"size"`
	FinishedAt string `json:"finishedAt"`
}

var resourceID = regexp.MustCompile(`^[a-f0-9-]{32,36}$`)

func ResourcePath(dataDir, id string) string { return filepath.Join(dataDir, "resources", id+".mp4") }

// Resources have independent files and records, deliberately without task foreign keys.
// A hard link avoids copying large immutable outputs while surviving task deletion.
func (s *Store) PublishResource(dataDir, source string, r Resource) error {
	if !resourceID.MatchString(r.ID) {
		return errors.New("资源 ID 无效")
	}
	dir := filepath.Join(dataDir, "resources")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	target := ResourcePath(dataDir, r.ID)
	created := false
	info, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		original, e := os.Lstat(source)
		if e != nil {
			return e
		}
		if !original.Mode().IsRegular() || original.Size() == 0 {
			return errors.New("视频文件无效")
		}
		if e = os.Link(source, target); e != nil {
			input, e := os.Open(source)
			if e != nil {
				return e
			}
			defer input.Close()
			temp, e := os.CreateTemp(dir, ".import-*")
			if e != nil {
				return e
			}
			defer os.Remove(temp.Name())
			_, e = io.Copy(temp, input)
			if e == nil {
				e = temp.Sync()
			}
			closeErr := temp.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				return e
			}
			if e = os.Rename(temp.Name(), target); e != nil {
				return e
			}
		}
		if e := syncResourceDir(dir); e != nil {
			return e
		}
		info, err = os.Lstat(target)
		created = true
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("视频资源文件无效")
	}
	// A failed final task-state write can leave an already-published resource.
	// A later successful retry must replace that attempt rather than serve it forever.
	previous, lookupErr := s.Resource(r.ID)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return lookupErr
	}
	// The file can also outlive a failed INSERT, leaving no resource row at all.
	// In that case it is not evidence of a successfully published attempt.
	if !created && (errors.Is(lookupErr, sql.ErrNoRows) || previous.FinishedAt != r.FinishedAt) {
		original, e := os.Lstat(source)
		if e != nil {
			return e
		}
		if !original.Mode().IsRegular() || original.Size() == 0 {
			return errors.New("视频文件无效")
		}
		if !os.SameFile(original, info) {
			input, e := os.Open(source)
			if e != nil {
				return e
			}
			defer input.Close()
			temp, e := os.CreateTemp(dir, ".retry-*")
			if e != nil {
				return e
			}
			defer os.Remove(temp.Name())
			_, e = io.Copy(temp, input)
			if e == nil {
				e = temp.Sync()
			}
			closeErr := temp.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				return e
			}
			if e = os.Rename(temp.Name(), target); e != nil {
				return e
			}
			if e = syncResourceDir(dir); e != nil {
				return e
			}
			info, e = os.Stat(target)
			if e != nil {
				return e
			}
		}
	}
	r.Size = info.Size()
	_, err = s.DB.Exec("INSERT INTO resources(id,name,task_id,task_name,size,finished_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,task_id=excluded.task_id,task_name=excluded.task_name,size=excluded.size,finished_at=excluded.finished_at", r.ID, r.Name, r.TaskID, r.TaskName, r.Size, r.FinishedAt)
	return err
}
func (s *Store) Resource(id string) (Resource, error) {
	var r Resource
	err := s.DB.QueryRow("SELECT id,name,task_id,task_name,size,finished_at FROM resources WHERE id=?", id).Scan(&r.ID, &r.Name, &r.TaskID, &r.TaskName, &r.Size, &r.FinishedAt)
	return r, err
}

// Windows cannot flush a directory opened through os.Open. File contents are
// synced before publication; directory durability is available on Unix.
func syncResourceDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
