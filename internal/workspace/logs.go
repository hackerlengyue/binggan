package workspace

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"time.haomen/binggan/v2/internal/store"
)

type ClientLog struct {
	ID      string `json:"id"`
	At      string `json:"at"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

type logInputError struct{ message string }

func (e *logInputError) Error() string { return e.message }
func invalidLog(message string) error  { return &logInputError{message} }

func (a *App) AppendLogs(input []ClientLog) error {
	if len(input) < 1 || len(input) > 100 {
		return invalidLog("日志条数须为 1–100")
	}
	entries := make([]store.Log, 0, len(input))
	for _, e := range input {
		if e.ID == "" || utf8.RuneCountInString(e.ID) > 100 || e.Message == "" || utf8.RuneCountInString(e.Message) > 20000 || utf8.RuneCountInString(e.Detail) > 100000 ||
			(e.Level != "info" && e.Level != "warn" && e.Level != "error") || (e.Source != "" && e.Source != "frontend" && e.Source != "connection") {
			return invalidLog("日志内容格式无效")
		}
		if e.Source == "" {
			e.Source = "connection"
		}
		at, err := time.Parse(time.RFC3339Nano, e.At)
		if err != nil {
			return invalidLog("日志时间无效")
		}
		entries = append(entries, store.Log{ID: e.ID, At: at.UTC().Format(time.RFC3339Nano), Level: e.Level, Source: e.Source, Message: e.Message, Detail: e.Detail})
	}
	return a.Store.InsertQueuedLogs(entries)
}

// SetLogClearer connects the native monitor's diagnostic file and buffer to the
// same clear operation as the persistent application logs.
func (a *App) SetLogClearer(clear func() error) {
	a.logClearMu.Lock()
	defer a.logClearMu.Unlock()
	a.logClearer = clear
}

func (a *App) ClearLogs(scope string) (int64, error) {
	if scope != "all" && scope != "connection" {
		return 0, invalidLog("日志清理范围无效")
	}
	if scope == "all" {
		a.logClearMu.RLock()
		clear := a.logClearer
		a.logClearMu.RUnlock()
		if clear != nil {
			// The live log writer uses SQLite too: clear it before opening a transaction.
			if err := clear(); err != nil {
				return 0, err
			}
		}
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	query := "DELETE FROM system_logs"
	key := "logs_cleared_at"
	if scope == "connection" {
		query = "DELETE FROM system_logs WHERE source='connection'"
		key = "connection_logs_cleared_at"
	}
	result, err := tx.Exec(query)
	if err == nil {
		_, err = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, store.Encode(time.Now().UTC()))
	}
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

type LogQuery struct {
	Level    string `json:"level"`
	Source   string `json:"source"`
	Search   string `json:"search"`
	From     string `json:"from"`
	To       string `json:"to"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type LogPage struct {
	Items    []store.Log `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

func logFilter(query LogQuery) (string, []any, error) {
	where := " WHERE 1=1"
	args := []any{}
	for _, field := range []struct{ key, value, allowed string }{
		{"level", query.Level, "|info|warn|error|"},
		{"source", query.Source, "|connection|server|request|decrypt|frontend|capture|certificate|"},
	} {
		v := field.value
		if v == "" || v == "all" {
			continue
		}
		if !strings.Contains(field.allowed, "|"+v+"|") {
			return "", nil, invalidLog("日志筛选参数无效")
		}
		where += " AND " + field.key + "=?"
		args = append(args, v)
	}
	if s := query.Search; s != "" {
		if len([]rune(s)) > 500 {
			return "", nil, invalidLog("搜索内容过长")
		}
		where += " AND (instr(lower(message),lower(?))>0 OR instr(lower(detail),lower(?))>0 OR instr(lower(request_id),lower(?))>0)"
		args = append(args, s, s, s)
	}
	var from, to time.Time
	for _, field := range []struct{ key, value string }{{"from", query.From}, {"to", query.To}} {
		if s := field.value; s != "" {
			v, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return "", nil, invalidLog("时间范围无效")
			}
			op := ">="
			if field.key == "to" {
				to = v
				op = "<="
			} else {
				from = v
			}
			where += " AND julianday(at)" + op + "julianday(?)"
			args = append(args, v.UTC().Format(time.RFC3339Nano))
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return "", nil, invalidLog("开始时间不能晚于结束时间")
	}
	return where, args, nil
}
func (a *App) readLogs(where string, args []any, limit, offset int) ([]store.Log, error) {
	args = append(append([]any{}, args...), limit, offset)
	rows, err := a.Store.DB.Query("SELECT id,at,level,source,message,detail,request_id FROM system_logs"+where+" ORDER BY rtrim(at,'Z') DESC,rowid DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]store.Log, 0)
	for rows.Next() {
		var l store.Log
		if err = rows.Scan(&l.ID, &l.At, &l.Level, &l.Source, &l.Message, &l.Detail, &l.RequestID); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}

// LogExport fixes the upper row boundary before the first byte is written.
// New logs cannot shift the export's pages while it is being downloaded.
type LogExport struct {
	app   *App
	where string
	args  []any
}

func (a *App) PrepareLogExport(query LogQuery) (*LogExport, error) {
	where, args, err := logFilter(query)
	if err != nil {
		return nil, err
	}
	var max int64
	if err = a.Store.DB.QueryRow("SELECT COALESCE(MAX(rowid),0) FROM system_logs").Scan(&max); err != nil {
		return nil, err
	}
	return &LogExport{app: a, where: where + " AND rowid<=?", args: append(args, max)}, nil
}

func (export *LogExport) Write(ctx context.Context, out io.Writer) error {
	for offset := 0; ; offset += 500 {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := export.app.readLogs(export.where, export.args, 500, offset)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			at, _ := time.Parse(time.RFC3339Nano, item.At)
			if _, err := fmt.Fprintf(out, "[%s] [%s] [%s] %s\n%s\n请求编号：%s\n\n", at.Local().Format("2006-01-02 15:04:05"), strings.ToUpper(item.Level), item.Source, item.Message, item.Detail, item.RequestID); err != nil {
				return err
			}
		}
		if len(items) < 500 {
			return nil
		}
	}
}
func (a *App) Logs(query LogQuery) (LogPage, error) {
	if query.Page < 1 || query.Page > 10000000 {
		return LogPage{}, invalidLog("页码无效")
	}
	if query.PageSize != 20 && query.PageSize != 50 && query.PageSize != 100 {
		return LogPage{}, invalidLog("每页条数须为 20、50 或 100")
	}
	where, args, err := logFilter(query)
	if err != nil {
		return LogPage{}, err
	}
	var total int
	if err = a.Store.DB.QueryRow("SELECT count(*) FROM system_logs"+where, args...).Scan(&total); err != nil {
		return LogPage{}, err
	}
	page := min(query.Page, max(1, (total+query.PageSize-1)/query.PageSize))
	items, err := a.readLogs(where, args, query.PageSize, (page-1)*query.PageSize)
	if err != nil {
		return LogPage{}, err
	}
	return LogPage{items, total, page, query.PageSize}, nil
}

var errJobNotFound = errors.New("子任务不存在")
var errInvalidLogOffset = errors.New("日志偏移无效")
var errLogOffsetBeyondSize = errors.New("日志偏移超过文件大小")

type JobLogChunk struct {
	Data    string `json:"data"`
	Offset  int64  `json:"offset"`
	HasMore bool   `json:"hasMore"`
}

func (a *App) jobLogSize(id string) (int64, error) {
	if _, ok := a.Tasks.Job(id); !ok {
		return 0, errJobNotFound
	}
	var size int64
	err := a.Store.DB.QueryRow("SELECT COALESCE(MAX(byte_start+length(data)),0) FROM job_logs WHERE job_id=?", id).Scan(&size)
	return size, err
}

func (a *App) readJobLog(id string, start int64) ([]byte, error) {
	rows, err := a.Store.DB.Query("SELECT byte_start,data FROM job_logs WHERE job_id=? AND byte_start+length(data)>? AND byte_start<? ORDER BY byte_start", id, start, start+(256<<10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]byte, 0, 256<<10)
	for rows.Next() {
		var at int64
		var b []byte
		if err = rows.Scan(&at, &b); err != nil {
			return nil, err
		}
		skip := int64(0)
		if start > at {
			skip = start - at
		}
		remain := (256 << 10) - len(out)
		b = b[skip:]
		if len(b) > remain {
			b = b[:remain]
		}
		out = append(out, b...)
		if len(out) == 256<<10 {
			break
		}
	}
	return out, rows.Err()
}

type JobLogExport struct {
	app  *App
	id   string
	size int64
}

func (export *JobLogExport) Size() int64 { return export.size }

func (a *App) PrepareJobLogExport(id string) (*JobLogExport, error) {
	size, err := a.jobLogSize(id)
	if err != nil {
		return nil, err
	}
	return &JobLogExport{app: a, id: id, size: size}, nil
}

func (export *JobLogExport) Write(ctx context.Context, out io.Writer) error {
	return export.WriteFrom(ctx, out, 0)
}

// WriteFrom retains the captured upper boundary while honoring byte offsets.
func (export *JobLogExport) WriteFrom(ctx context.Context, out io.Writer, offset int64) error {
	if offset < 0 {
		return errInvalidLogOffset
	}
	if offset > export.size {
		return errLogOffsetBeyondSize
	}
	for pos := offset; pos < export.size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk, err := export.app.readJobLog(export.id, pos)
		if err != nil {
			return err
		}
		if len(chunk) == 0 {
			return io.ErrUnexpectedEOF
		}
		if pos+int64(len(chunk)) > export.size {
			chunk = chunk[:export.size-pos]
		}
		n, err := out.Write(chunk)
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrShortWrite
		}
		pos += int64(n)
	}
	return nil
}

func (a *App) JobLogs(id string, offset int64) (JobLogChunk, error) {
	if offset < 0 {
		return JobLogChunk{}, errInvalidLogOffset
	}
	size, err := a.jobLogSize(id)
	if err != nil {
		return JobLogChunk{}, err
	}
	if offset > size {
		return JobLogChunk{}, errLogOffsetBeyondSize
	}
	b, err := a.readJobLog(id, offset)
	if err != nil {
		return JobLogChunk{}, err
	}
	next := offset + int64(len(b))
	return JobLogChunk{base64.StdEncoding.EncodeToString(b), next, next < size}, nil
}
