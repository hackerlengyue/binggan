package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
)

// downloadManager lets MyGo transfer attachments while the native save dialog
// chooses the destination. A same-directory staging file keeps an existing
// download intact if the transfer fails.
type downloadManager struct {
	window  *mygo.Window
	choose  func(mygo.SaveDialogOptions) (string, error)
	report  func(error)
	mu      sync.Mutex
	pending map[string]string
}

func installDownloads(window *mygo.Window) *downloadManager {
	m := &downloadManager{
		window:  window,
		choose:  mygo.Dialog.Save,
		pending: make(map[string]string),
		report: func(err error) {
			slog.Error("保存下载文件失败", "error", err)
			go func() {
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Parent: window, Type: mygo.MessageError, Title: "下载失败",
					Message: "文件未能下载或保存，请确认资源仍存在，并检查目标目录的空间与权限后重试。",
				})
			}()
		},
	}
	window.Page().OnWillDownload(m.willDownload)
	window.Page().OnDownloadDone(m.downloadDone)
	return m
}

func (m *downloadManager) willDownload(event *mygo.DownloadEvent) {
	defaultPath := event.SuggestedName
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil {
		defaultPath = filepath.Join(dir, event.SuggestedName)
	}
	dest, err := m.choose(mygo.SaveDialogOptions{
		Parent: m.window, Title: "保存文件", DefaultPath: defaultPath,
	})
	if err != nil || dest == "" {
		event.PreventDefault()
		if err != nil {
			m.report(err)
		}
		return
	}
	dest = filepath.Clean(dest)
	file, err := os.CreateTemp(filepath.Dir(dest), ".binggan-download-*")
	if err != nil {
		event.PreventDefault()
		m.report(fmt.Errorf("无法准备下载文件：%w", err))
		return
	}
	staged := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(staged)
		event.PreventDefault()
		m.report(fmt.Errorf("无法关闭下载临时文件：%w", err))
		return
	}
	// WebKit rejects a download destination that already exists.
	if err := os.Remove(staged); err != nil {
		event.PreventDefault()
		m.report(fmt.Errorf("无法准备下载路径：%w", err))
		return
	}
	m.mu.Lock()
	m.pending[staged] = dest
	m.mu.Unlock()
	event.Path = staged
}

func (m *downloadManager) downloadDone(download *mygo.Download) {
	m.mu.Lock()
	dest, ok := m.pending[download.Path]
	delete(m.pending, download.Path)
	m.mu.Unlock()
	if !ok {
		if download.Err != nil {
			m.report(download.Err)
		}
		return
	}
	if download.Err != nil {
		_ = os.Remove(download.Path)
		m.report(download.Err)
		return
	}
	if err := replaceDownload(download.Path, dest); err != nil {
		_ = os.Remove(download.Path)
		m.report(fmt.Errorf("无法完成下载文件：%w", err))
	}
}

func (m *downloadManager) close() {
	m.mu.Lock()
	paths := make([]string, 0, len(m.pending))
	for staged := range m.pending {
		paths = append(paths, staged)
	}
	clear(m.pending)
	m.mu.Unlock()
	for _, staged := range paths {
		_ = os.Remove(staged)
	}
}
