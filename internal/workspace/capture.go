package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time.haomen/binggan/v2/internal/store"
)

// CaptureNotice is the typed event delivered to one MyGo Channel subscriber.
type CaptureNotice struct {
	Kind     string   `json:"kind"`
	Capture  *Capture `json:"capture,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
	ClientID string   `json:"clientId,omitempty"`
}
type hub struct {
	mu      sync.Mutex
	clients map[chan CaptureNotice]bool
	closed  bool
}

func newHub() *hub { return &hub{clients: map[chan CaptureNotice]bool{}} }
func (h *hub) add() (chan CaptureNotice, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) >= 32 {
		return nil, false
	}
	ch := make(chan CaptureNotice, 128)
	h.clients[ch] = true
	return ch, true
}
func (h *hub) remove(ch chan CaptureNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[ch] {
		delete(h.clients, ch)
		close(ch)
	}
}
func (h *hub) broadcast(e CaptureNotice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- e:
		default:
			delete(h.clients, ch)
			close(ch)
		}
	}
}
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}
func (h *hub) count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.clients) }

var errSubscriberLimit = errors.New("实时连接数量已达上限")

// Subscribe sends a snapshot before queued live events. Closing the calling
// page cancels ctx; the hub also closes slow clients whose buffer is full.
func (a *App) Subscribe(ctx context.Context, send func(CaptureNotice) error) error {
	a.captureMu.Lock()
	ch, ok := a.hub.add()
	enabled := a.capture
	a.captureMu.Unlock()
	if !ok {
		return errSubscriberLimit
	}
	defer a.hub.remove(ch)
	if err := send(CaptureNotice{Kind: "ready"}); err != nil {
		return err
	}
	if err := send(CaptureNotice{Kind: "capture_state", Enabled: &enabled}); err != nil {
		return err
	}
	for {
		select {
		case <-a.ctx.Done():
			return nil
		case <-ctx.Done():
			return nil
		case e, open := <-ch:
			if !open {
				return nil
			}
			if err := send(e); err != nil {
				return err
			}
		}
	}
}

type Capture struct {
	ID            string         `json:"id"`
	RequestID     string         `json:"requestId,omitempty" binding:"max=100"`
	Phase         string         `json:"phase" binding:"required,oneof=request response"`
	TS            string         `json:"ts"`
	Method        string         `json:"method" binding:"required,max=20"`
	URL           string         `json:"url" binding:"required,max=20000"`
	Host          string         `json:"host" binding:"required,max=253"`
	Status        *int           `json:"status,omitempty"`
	Headers       map[string]any `json:"headers"`
	Body          string         `json:"body"`
	BodyTruncated bool           `json:"bodyTruncated"`
	Source        string         `json:"source"`
}

func (a *App) CaptureState() bool {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	return a.capture
}

func (a *App) SetCaptureState(enabled bool) bool {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	a.capture = enabled
	a.hub.broadcast(CaptureNotice{Kind: "capture_state", Enabled: &enabled})
	return enabled
}

// RecentCaptureJSON returns stored capture records without discarding fields
// unknown to the current Capture DTO. Exports use this to preserve 1.x data.
func (a *App) RecentCaptureJSON(limit int) ([]json.RawMessage, error) {
	if limit < 1 || limit > 2500 {
		return nil, errInvalidHistoryLimit
	}
	rows, err := a.Store.DB.Query("SELECT payload FROM captures ORDER BY seq DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]json.RawMessage, 0)
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		items = append(items, json.RawMessage(raw))
	}
	return items, rows.Err()
}

var errInvalidHistoryLimit = errors.New("limit 须为 1–2500")

func (a *App) History(limit int) ([]Capture, error) {
	raw, err := a.RecentCaptureJSON(limit)
	if err != nil {
		return nil, err
	}
	items := make([]Capture, 0, len(raw))
	for _, data := range raw {
		var item Capture
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

type HealthStatus struct {
	OK               bool    `json:"ok"`
	Count            int     `json:"count"`
	CaptureEnabled   bool    `json:"captureEnabled"`
	LastReceivedAt   *string `json:"lastReceivedAt"`
	ConnectedClients int     `json:"connectedClients"`
	ServerTime       string  `json:"serverTime"`
	Runtime          string  `json:"runtime"`
}

func (a *App) Health() (HealthStatus, error) {
	var count int
	var last *string
	err := a.Store.DB.QueryRow("SELECT MIN(COUNT(*),2500),json_extract((SELECT payload FROM captures ORDER BY seq DESC LIMIT 1),'$.ts') FROM captures").Scan(&count, &last)
	if err != nil {
		return HealthStatus{}, err
	}
	return HealthStatus{true, count, a.CaptureState(), last, a.hub.count(), store.Now(), "go"}, nil
}

var errClientIDTooLong = errors.New("提取客户端标识过长")
var errCaptureRunning = errors.New("请先停止提取再清空记录")

func (a *App) ClearTraces(clientID string) error {
	if len(clientID) > 100 {
		return errClientIDTooLong
	}
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	if a.capture {
		return errCaptureRunning
	}
	if _, err := a.Store.DB.Exec("DELETE FROM captures"); err != nil {
		return err
	}
	a.hub.broadcast(CaptureNotice{Kind: "clear", ClientID: clientID})
	return nil
}
