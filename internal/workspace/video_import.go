package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"time.haomen/binggan/v2/internal/store"
)

// LocalVideo describes a file selected with the native dialog or dropped on
// the window. The path remains local to the page and is rechecked on import.
type LocalVideo struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	ModifiedTime int64  `json:"modifiedTime"`
	Error        string `json:"error,omitempty"`
}

type VideoImportProgress struct {
	Copied int64 `json:"copied"`
	Total  int64 `json:"total"`
}

type ImportedVideo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (a *App) inspectVideo(path string) (LocalVideo, os.FileInfo) {
	item := LocalVideo{Path: path, Name: filepath.Base(path)}
	if !filepath.IsAbs(path) || path == "" || !strings.EqualFold(filepath.Ext(item.Name), ".sz") {
		item.Error = "请选择 .sz 格式的视频文件。"
		return item, nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		item.Error = "文件不存在或不是普通文件，请重新选择。"
		return item, nil
	}
	item.Size = info.Size()
	item.ModifiedTime = info.ModTime().UnixMilli()
	if item.Size == 0 {
		item.Error = "这个文件是空的，请重新选择。"
	} else if item.Size > a.Config.MaxUploadBytes {
		item.Error = "视频超过 20 GiB 限制。"
	}
	return item, info
}

func (a *App) InspectVideoFiles(paths []string) ([]LocalVideo, error) {
	if len(paths) > 1000 {
		return nil, errors.New("一次选择的视频过多，请分批选择")
	}
	items := make([]LocalVideo, 0, len(paths))
	for _, path := range paths {
		item, _ := a.inspectVideo(path)
		items = append(items, item)
	}
	return items, nil
}

// ImportVideo copies one selected file into the private task area. Closing the
// MyGo Channel cancels ctx, which removes a partial copy before returning.
func (a *App) ImportVideo(ctx context.Context, choice LocalVideo, progress func(VideoImportProgress) error) (ImportedVideo, error) {
	item, original := a.inspectVideo(choice.Path)
	if item.Error != "" {
		return ImportedVideo{}, errors.New(item.Error)
	}
	if item.Name != choice.Name || item.Size != choice.Size || item.ModifiedTime != choice.ModifiedTime {
		return ImportedVideo{}, errors.New("视频文件已发生变化，请重新选择")
	}
	if err := ctx.Err(); err != nil {
		return ImportedVideo{}, err
	}
	select {
	case a.uploads <- struct{}{}:
		defer func() { <-a.uploads }()
	default:
		return ImportedVideo{}, errors.New("导入繁忙，请稍后重试")
	}
	source, err := os.Open(choice.Path)
	if err != nil {
		return ImportedVideo{}, fmt.Errorf("无法读取视频：%w", err)
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(original, opened) {
		return ImportedVideo{}, errors.New("视频文件已发生变化，请重新选择")
	}
	id := store.ID()
	dir := a.Tasks.Path(id, "")
	if err := os.Mkdir(dir, 0700); err != nil {
		return ImportedVideo{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	target, err := os.OpenFile(filepath.Join(dir, "input.sz"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ImportedVideo{}, err
	}
	defer target.Close()
	if progress != nil {
		if err := progress(VideoImportProgress{Total: item.Size}); err != nil {
			return ImportedVideo{}, err
		}
	}
	buffer := make([]byte, 1<<20)
	var copied int64
	lastProgress := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return ImportedVideo{}, err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			copied += int64(n)
			if copied > a.Config.MaxUploadBytes || copied > item.Size {
				return ImportedVideo{}, errors.New("视频文件在导入时发生变化，请重新选择")
			}
			written, writeErr := target.Write(buffer[:n])
			if writeErr != nil {
				return ImportedVideo{}, writeErr
			}
			if written != n {
				return ImportedVideo{}, io.ErrShortWrite
			}
			if progress != nil && (time.Since(lastProgress) >= 200*time.Millisecond || copied == item.Size) {
				if err := progress(VideoImportProgress{Copied: copied, Total: item.Size}); err != nil {
					return ImportedVideo{}, err
				}
				lastProgress = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ImportedVideo{}, readErr
		}
	}
	if copied != item.Size {
		return ImportedVideo{}, errors.New("视频文件在导入时发生变化，请重新选择")
	}
	final, err := os.Lstat(choice.Path)
	if err != nil || !os.SameFile(original, final) || final.Size() != item.Size || final.ModTime().UnixMilli() != item.ModifiedTime {
		return ImportedVideo{}, errors.New("视频文件在导入时发生变化，请重新选择")
	}
	if err := ctx.Err(); err != nil {
		return ImportedVideo{}, err
	}
	if err := target.Sync(); err != nil {
		return ImportedVideo{}, err
	}
	if err := target.Close(); err != nil {
		return ImportedVideo{}, err
	}
	if err := a.Tasks.AddUpload(id, item.Name, target.Name()); err != nil {
		return ImportedVideo{}, err
	}
	keep = true
	return ImportedVideo{ID: id, Name: item.Name}, nil
}
