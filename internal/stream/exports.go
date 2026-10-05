package stream

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"time.haomen/binggan/v2/internal/workspace"
)

func attachment(w http.ResponseWriter, kind, name string) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func archiveName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "视频"
	}
	return name
}

func (h *Handler) serveJob(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", "attachment")
	job, ok := h.workspace.Tasks.Job(r.PathValue("id"))
	if !ok {
		fail(w, http.StatusNotFound, "子任务不存在")
		return
	}
	if job.Status != "completed" {
		fail(w, http.StatusConflict, "视频尚未解密完成")
		return
	}
	file, err := os.Open(h.workspace.Tasks.Path(job.ID, "output.mp4"))
	if err != nil {
		fail(w, http.StatusNotFound, "结果文件不存在")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, http.StatusNotFound, "结果文件不存在")
		return
	}
	name := strings.TrimSuffix(job.Name, filepath.Ext(job.Name)) + ".mp4"
	attachment(w, "video/mp4", name)
	http.ServeContent(w, r, name, info.ModTime(), file)
}

type archiveEntry struct {
	name string
	file *os.File
	info os.FileInfo
}

type contextReader struct {
	ctx context.Context
	in  io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.in.Read(buffer)
}

func (h *Handler) serveTask(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", "attachment")
	task, ok := h.workspace.Tasks.Task(r.PathValue("id"))
	if !ok {
		fail(w, http.StatusNotFound, "任务不存在")
		return
	}
	entries := make([]archiveEntry, 0, len(task.Children))
	defer func() {
		for _, entry := range entries {
			_ = entry.file.Close()
		}
	}()
	used := map[string]bool{}
	for _, job := range task.Children {
		if job.Status != "completed" {
			continue
		}
		file, err := os.Open(h.workspace.Tasks.Path(job.ID, "output.mp4"))
		if err != nil {
			fail(w, http.StatusConflict, "部分结果文件已丢失，请检查任务")
			return
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			fail(w, http.StatusConflict, "结果文件不可读取")
			return
		}
		name := archiveName(strings.TrimSuffix(job.Name, filepath.Ext(job.Name))) + ".mp4"
		base := strings.TrimSuffix(name, ".mp4")
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s (%d).mp4", base, n)
		}
		used[strings.ToLower(name)] = true
		entries = append(entries, archiveEntry{name, file, info})
	}
	if len(entries) == 0 {
		fail(w, http.StatusConflict, "暂无已完成的视频")
		return
	}
	attachment(w, "application/zip", archiveName(task.Name)+".zip")
	writer := zip.NewWriter(w)
	for _, entry := range entries {
		if r.Context().Err() != nil {
			return
		}
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetModTime(entry.info.ModTime())
		out, err := writer.CreateHeader(header)
		if err != nil {
			slog.Error("无法生成任务下载压缩包", "error", err)
			return
		}
		if _, err := io.Copy(out, contextReader{r.Context(), entry.file}); err != nil {
			slog.Error("任务下载中断", "error", err)
			return
		}
	}
	if err := writer.Close(); err != nil {
		slog.Error("无法完成任务下载压缩包", "error", err)
	}
}

func (h *Handler) serveLogs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("download") != "1" {
		fail(w, http.StatusBadRequest, "仅支持日志下载")
		return
	}
	w.Header().Set("Content-Disposition", "attachment")
	query := workspace.LogQuery{Level: r.URL.Query().Get("level"), Source: r.URL.Query().Get("source"), Search: r.URL.Query().Get("q"), From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")}
	export, err := h.workspace.PrepareLogExport(query)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	attachment(w, "text/plain; charset=utf-8", "logs.txt")
	if err = export.Write(r.Context(), w); err != nil && !errors.Is(err, r.Context().Err()) {
		slog.Error("日志导出中断", "error", err)
	}
}

func (h *Handler) serveJobLogs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("download") != "1" {
		fail(w, http.StatusBadRequest, "仅支持任务日志下载")
		return
	}
	w.Header().Set("Content-Disposition", "attachment")
	offset := int64(0)
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			fail(w, http.StatusBadRequest, "日志偏移无效")
			return
		}
		offset = value
	}
	id := r.PathValue("id")
	if _, ok := h.workspace.Tasks.Job(id); !ok {
		fail(w, http.StatusNotFound, "子任务不存在")
		return
	}
	export, err := h.workspace.PrepareJobLogExport(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, "任务日志读取失败")
		return
	}
	if offset < 0 || offset > export.Size() {
		fail(w, http.StatusBadRequest, "日志偏移超过文件大小")
		return
	}
	attachment(w, "text/plain; charset=utf-8", "task-"+id+".log")
	if err = export.WriteFrom(r.Context(), w, offset); err != nil && !errors.Is(err, r.Context().Err()) {
		slog.Error("任务日志导出中断", "error", err)
	}
}

func (h *Handler) serveCaptureExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", "attachment")
	items, err := h.workspace.RecentCaptureJSON(2500)
	if err != nil {
		fail(w, http.StatusInternalServerError, "接收记录读取失败")
		return
	}
	if r.URL.Query().Get("format") == "jsonl" {
		attachment(w, "application/x-ndjson; charset=utf-8", "captures.jsonl")
		for _, item := range items {
			if r.Context().Err() != nil {
				return
			}
			if _, err := fmt.Fprintln(w, string(item)); err != nil {
				return
			}
		}
		return
	}
	attachment(w, "application/json; charset=utf-8", "captures.json")
	_ = json.NewEncoder(w).Encode(items)
}
