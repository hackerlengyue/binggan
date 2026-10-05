package collectorhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"time.haomen/binggan/v2/internal/workspace"
)

type fakeCollector struct{ reads, rotations int }

func (f *fakeCollector) CollectorDescriptor() (workspace.CollectorDescriptor, error) {
	f.reads++
	return workspace.CollectorDescriptor{Version: 2, ProxyPassword: "private"}, nil
}
func (f *fakeCollector) RegenerateCollectorCertificate() (workspace.CollectorDescriptor, error) {
	f.rotations++
	return workspace.CollectorDescriptor{Version: 2, Fingerprint: "rotated"}, nil
}

func collectorRequest(handler http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:18778"+path, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestCollectorHandlerOnlyExposesAuthenticatedHelperRoutes(t *testing.T) {
	fake := &fakeCollector{}
	h := New(fake, strings.Repeat("a", 64))
	token := map[string]string{"X-Capture-Token": strings.Repeat("a", 64)}
	for _, path := range []string{"/api/health", "/api/history", "/api/events", "/api/decrypt", "/api/resources", "/"} {
		if got := collectorRequest(h, "GET", path, token); got.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", path, got.Code)
		}
	}
	if got := collectorRequest(h, "GET", "/api/collector", nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("missing token returned %d", got.Code)
	}
	if got := collectorRequest(h, "GET", "/api/collector", map[string]string{"X-Capture-Token": "wrong"}); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token returned %d", got.Code)
	}
	if fake.reads != 0 || fake.rotations != 0 {
		t.Fatalf("unauthorized requests reached collector: %+v", fake)
	}
	got := collectorRequest(h, "GET", "/api/collector", token)
	if got.Code != http.StatusOK || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated descriptor: %d, %q", got.Code, got.Header().Get("Cache-Control"))
	}
	var descriptor workspace.CollectorDescriptor
	if err := json.Unmarshal(got.Body.Bytes(), &descriptor); err != nil || descriptor.ProxyPassword != "private" {
		t.Fatalf("descriptor response: %+v, %v", descriptor, err)
	}
	got = collectorRequest(h, "POST", "/api/collector/certificate", token)
	if got.Code != http.StatusOK || fake.reads != 1 || fake.rotations != 1 {
		t.Fatalf("authorized methods: %d, %+v", got.Code, fake)
	}
}

func TestCollectorHandlerRejectsOtherOriginsHostsAndMethods(t *testing.T) {
	fake := &fakeCollector{}
	h := New(fake, "fixture-token")
	token := map[string]string{"X-Capture-Token": "fixture-token"}
	for _, headers := range []map[string]string{
		{"X-Capture-Token": "fixture-token", "Origin": "https://example.test"},
		{"X-Capture-Token": "fixture-token", "Sec-Fetch-Site": "cross-site"},
	} {
		if got := collectorRequest(h, "GET", "/api/collector", headers); got.Code != http.StatusForbidden {
			t.Fatalf("foreign origin returned %d", got.Code)
		}
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Host = "example.test" },
		func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1234" },
		func(r *http.Request) { r.Header.Add("X-Capture-Token", "duplicate") },
	} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:18778/api/collector", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Capture-Token", "fixture-token")
		change(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == http.StatusOK {
			t.Fatal("altered request reached collector")
		}
	}
	if got := collectorRequest(h, "POST", "/api/collector", token); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "GET" {
		t.Fatalf("wrong method returned %d with Allow %q", got.Code, got.Header().Get("Allow"))
	}
	if got := collectorRequest(h, "GET", "/api/collector/certificate", token); got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != "POST" {
		t.Fatalf("wrong method returned %d with Allow %q", got.Code, got.Header().Get("Allow"))
	}
	if fake.reads != 0 || fake.rotations != 0 {
		t.Fatalf("rejected requests reached collector: %+v", fake)
	}
	if got := collectorRequest(New(fake, ""), "GET", "/api/collector", token); got.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured token returned %d", got.Code)
	}
}
