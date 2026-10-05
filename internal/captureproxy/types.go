package captureproxy

import "strings"

type Event struct {
	Phase         string            `json:"phase"`
	TS            string            `json:"ts"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Host          string            `json:"host"`
	Status        int               `json:"status,omitempty"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"bodyTruncated"`
	Source        string            `json:"source"`
}
type Pair struct {
	Request  Event
	Response Event
	Path     string
}
type Sink interface {
	Enabled() bool
	Accept(Pair)
	Log(level, message, detail string)
}

func TargetHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	return h == "shenzaokeji.com" || strings.HasSuffix(h, ".shenzaokeji.com")
}
