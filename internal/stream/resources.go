// Package stream serves app-owned media bytes through MyGo Protocol.
package stream

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"time.haomen/binggan/v2/internal/procutil"
	"time.haomen/binggan/v2/internal/store"
	"time.haomen/binggan/v2/internal/workspace"
)

type thumbnailJob struct {
	done chan struct{}
	err  error
}

type Handler struct {
	store          *store.Store
	workspace      *workspace.App
	dataDir        string
	ctx            context.Context
	routes         *http.ServeMux
	thumbnailMu    sync.Mutex
	thumbnailJobs  map[string]*thumbnailJob
	thumbnailSlots chan struct{}
}

func New(ctx context.Context, data *store.Store, dataDir string, app *workspace.App) *Handler {
	h := &Handler{
		store: data, dataDir: dataDir, ctx: ctx, workspace: app,
		routes: http.NewServeMux(), thumbnailJobs: make(map[string]*thumbnailJob),
		thumbnailSlots: make(chan struct{}, 2),
	}
	h.routes.HandleFunc("GET /api/resources/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		h.serveResource(w, r, false)
	})
	h.routes.HandleFunc("GET /api/resources/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		h.serveResource(w, r, true)
	})
	h.routes.HandleFunc("GET /api/resources/{id}/thumbnail", h.serveThumbnail)
	h.routes.HandleFunc("GET /api/decrypt/tasks/{id}/download", h.serveTask)
	h.routes.HandleFunc("GET /api/decrypt/jobs/{id}/download", h.serveJob)
	h.routes.HandleFunc("GET /api/decrypt/jobs/{id}/logs", h.serveJobLogs)
	h.routes.HandleFunc("GET /api/logs", h.serveLogs)
	h.routes.HandleFunc("GET /api/export", h.serveCaptureExport)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.routes.ServeHTTP(w, r)
}

func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": message})
}

func (h *Handler) resource(id string) (store.Resource, string, os.FileInfo, error) {
	resource, err := h.store.Resource(id)
	if err != nil {
		return store.Resource{}, "", nil, err
	}
	path := store.ResourcePath(h.dataDir, resource.ID)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return store.Resource{}, "", nil, os.ErrNotExist
	}
	return resource, path, info, nil
}

func (h *Handler) serveResource(w http.ResponseWriter, r *http.Request, download bool) {
	if download {
		// MyGo handles attachment responses through download events, including
		// failures. Without this header a 404 replaces the page with JSON.
		w.Header().Set("Content-Disposition", "attachment")
	}
	resource, path, _, err := h.resource(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "视频资源不存在")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "结果文件不存在")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "视频资源读取失败")
		return
	}
	file, err := os.Open(path)
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
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": resource.Name}))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, resource.Name, info.ModTime(), file)
}

func thumbnailIdentity(id string, info os.FileInfo) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", id, info.Size(), info.ModTime().UnixNano()))))
}

func (h *Handler) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	resource, source, info, err := h.resource(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusNotFound, "视频资源不存在")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "结果文件不存在")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "视频资源读取失败")
		return
	}
	key := thumbnailIdentity(resource.ID, info)
	target := filepath.Join(h.dataDir, "cache", "resource-thumbnails", key+".jpg")
	if cached, err := os.Lstat(target); err != nil || !cached.Mode().IsRegular() || cached.Size() == 0 {
		h.thumbnailMu.Lock()
		job := h.thumbnailJobs[key]
		if job == nil {
			job = &thumbnailJob{done: make(chan struct{})}
			h.thumbnailJobs[key] = job
			go h.generate(job, key, source, target)
		}
		h.thumbnailMu.Unlock()
		select {
		case <-r.Context().Done():
			return
		case <-job.done:
			if job.err != nil {
				fail(w, http.StatusServiceUnavailable, "视频预览暂不可用")
				return
			}
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", `"`+key+`"`)
	if r.URL.Query().Get("v") == resource.FinishedAt {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	file, err := os.Open(target)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "视频预览暂不可用")
		return
	}
	defer file.Close()
	cached, err := file.Stat()
	if err != nil || !cached.Mode().IsRegular() {
		fail(w, http.StatusServiceUnavailable, "视频预览暂不可用")
		return
	}
	http.ServeContent(w, r, resource.Name+".jpg", cached.ModTime(), file)
}

func (h *Handler) generate(job *thumbnailJob, key, source, target string) {
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	select {
	case h.thumbnailSlots <- struct{}{}:
		job.err = generateThumbnail(ctx, source, target)
		<-h.thumbnailSlots
	case <-ctx.Done():
		job.err = ctx.Err()
	}
	h.thumbnailMu.Lock()
	delete(h.thumbnailJobs, key)
	close(job.done)
	h.thumbnailMu.Unlock()
}

func generateThumbnail(ctx context.Context, source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".thumbnail-*.jpg")
	if err != nil {
		return err
	}
	name := temp.Name()
	if err = temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	defer os.Remove(name)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-ss", "0.1", "-i", source, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=480:-2", "-threads", "1", "-q:v", "6", "-f", "image2", "-update", "1", "-y", name)
	procutil.HideWindow(cmd)
	if err = cmd.Run(); err != nil {
		return err
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	_, err = jpeg.DecodeConfig(f)
	f.Close()
	if err != nil {
		return err
	}
	return os.Rename(name, target)
}
