package stream

import (
	"bytes"
	"context"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"time.haomen/binggan/v2/internal/store"
)

func resourceFixture(t *testing.T, contents []byte) (*Handler, store.Resource, string) {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	source := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(source, contents, 0600); err != nil {
		t.Fatal(err)
	}
	resource := store.Resource{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Name: "教学视频.mp4", FinishedAt: store.Now()}
	if err := db.PublishResource(dataDir, source, resource); err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), db, dataDir, nil), resource, dataDir
}

func mediaRequest(h *Handler, method, path, rangeHeader string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, nil)
	if rangeHeader != "" {
		r.Header.Set("Range", rangeHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestResourceRangeHeadDownloadAndMissingFile(t *testing.T) {
	h, resource, dataDir := resourceFixture(t, []byte("0123456789"))
	base := "/api/resources/" + resource.ID
	partial := mediaRequest(h, "GET", base+"/stream", "bytes=2-5")
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "2345" || partial.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range response = %d %q %q", partial.Code, partial.Body.String(), partial.Header().Get("Content-Range"))
	}
	head := mediaRequest(h, "HEAD", base+"/stream", "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
		t.Fatalf("HEAD response = %d bytes=%d length=%q", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}
	invalid := mediaRequest(h, "GET", base+"/stream", "bytes=100-")
	if invalid.Code != http.StatusRequestedRangeNotSatisfiable || invalid.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("unsatisfiable range = %d %q", invalid.Code, invalid.Header().Get("Content-Range"))
	}
	download := mediaRequest(h, "GET", base+"/download", "")
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), []byte("0123456789")) || download.Header().Get("Content-Disposition") == "" {
		t.Fatalf("download response = %d %q", download.Code, download.Header().Get("Content-Disposition"))
	}
	if missing := mediaRequest(h, "GET", "/api/resources/missing/stream", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown ID returned %d", missing.Code)
	}
	if missing := mediaRequest(h, "GET", "/api/resources/missing/download", ""); missing.Code != http.StatusNotFound || missing.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("unknown attachment = %d, disposition=%q", missing.Code, missing.Header().Get("Content-Disposition"))
	}
	target := store.ResourcePath(dataDir, resource.ID)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dataDir, "outside.mp4"), target); err != nil {
		t.Fatal(err)
	}
	if linked := mediaRequest(h, "GET", base+"/stream", ""); linked.Code != http.StatusNotFound {
		t.Fatalf("symlink returned %d", linked.Code)
	}
	if linked := mediaRequest(h, "GET", base+"/download", ""); linked.Code != http.StatusNotFound || linked.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("missing attachment = %d, disposition=%q", linked.Code, linked.Header().Get("Content-Disposition"))
	}
}

func TestResourceThumbnailCoalescesAndCaches(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	source := filepath.Join(t.TempDir(), "video.mp4")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-f", "lavfi", "-i", "color=c=blue:s=160x90:d=1", "-an", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-movflags", "+faststart", source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("video fixture: %v %s", err, out)
	}
	video, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	h, resource, dataDir := resourceFixture(t, video)
	path := "/api/resources/" + resource.ID + "/thumbnail"
	responses := make([]*httptest.ResponseRecorder, 3)
	var workers sync.WaitGroup
	for i := range responses {
		workers.Add(1)
		go func() {
			defer workers.Done()
			responses[i] = mediaRequest(h, "GET", path, "")
		}()
	}
	workers.Wait()
	for _, response := range responses {
		if response.Code != http.StatusOK {
			t.Fatalf("thumbnail returned %d: %s", response.Code, response.Body.String())
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(response.Body.Bytes()))
		if err != nil || cfg.Width != 480 || cfg.Height != 270 {
			t.Fatalf("thumbnail dimensions = %+v, %v", cfg, err)
		}
		if !bytes.Equal(response.Body.Bytes(), responses[0].Body.Bytes()) {
			t.Fatal("parallel thumbnail results differ")
		}
	}
	files, err := os.ReadDir(filepath.Join(dataDir, "cache", "resource-thumbnails"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files = %d, %v", len(files), err)
	}
	revision := mediaRequest(h, "GET", path+"?v="+url.QueryEscape(resource.FinishedAt), "")
	if revision.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatal("revision did not get stable cache policy")
	}
	r := httptest.NewRequest("GET", "http://localhost"+path, nil)
	r.Header.Set("If-None-Match", responses[0].Header().Get("ETag"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Fatalf("matching ETag returned %d", w.Code)
	}
	resourcePath := store.ResourcePath(dataDir, resource.ID)
	info, err := os.Stat(resourcePath)
	if err != nil {
		t.Fatal(err)
	}
	updatedTime := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(resourcePath, updatedTime, updatedTime); err != nil {
		t.Fatal(err)
	}
	updated := mediaRequest(h, "GET", path, "")
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") == responses[0].Header().Get("ETag") {
		t.Fatalf("modified video kept stale thumbnail: %d %q", updated.Code, updated.Header().Get("ETag"))
	}
}

func TestInvalidVideoDoesNotLeaveThumbnailCache(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg not installed")
	}
	h, resource, dataDir := resourceFixture(t, []byte("not an MP4 video"))
	path := "/api/resources/" + resource.ID + "/thumbnail"
	for range 2 {
		response := mediaRequest(h, "GET", path, "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("invalid video thumbnail = %d: %s", response.Code, response.Body.String())
		}
		files, err := os.ReadDir(filepath.Join(dataDir, "cache", "resource-thumbnails"))
		if err != nil || len(files) != 0 {
			t.Fatalf("invalid video left thumbnail cache: %d files, %v", len(files), err)
		}
	}
}
