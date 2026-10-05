// Package collectorhttp is the sole loopback HTTP surface used by the
// privileged capture helper. Application pages use MyGo bindings and Protocol.
package collectorhttp

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"

	"time.haomen/binggan/v2/internal/workspace"
)

type handler struct {
	app   collector
	token string
}

type collector interface {
	CollectorDescriptor() (workspace.CollectorDescriptor, error)
	RegenerateCollectorCertificate() (workspace.CollectorDescriptor, error)
}

func New(app collector, token string) http.Handler {
	return &handler{app: app, token: token}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !localRequest(r) || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		respondError(w, http.StatusForbidden, "请求来源不允许")
		return
	}
	if r.URL.Path != "/api/collector" && r.URL.Path != "/api/collector/certificate" {
		respondError(w, http.StatusNotFound, "接口不存在")
		return
	}
	if r.URL.Path == "/api/collector" && r.Method != http.MethodGet ||
		r.URL.Path == "/api/collector/certificate" && r.Method != http.MethodPost {
		w.Header().Set("Allow", map[string]string{
			"/api/collector":             "GET",
			"/api/collector/certificate": "POST",
		}[r.URL.Path])
		respondError(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	tokens := r.Header.Values("X-Capture-Token")
	if len(tokens) != 1 || h.token == "" || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(h.token)) != 1 {
		respondError(w, http.StatusUnauthorized, "接收令牌不匹配")
		return
	}
	var descriptor workspace.CollectorDescriptor
	var err error
	if r.Method == http.MethodPost {
		descriptor, err = h.app.RegenerateCollectorCertificate()
	} else {
		descriptor, err = h.app.CollectorDescriptor()
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "抓包入口暂不可用")
		return
	}
	_ = json.NewEncoder(w).Encode(descriptor)
}

func localRequest(r *http.Request) bool {
	if r.Host != "127.0.0.1" {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && host == "127.0.0.1"
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
