package capture

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Connect imports existing rotating JSON logs before attaching the live sink.
// Call it once, before publishing the logbook to monitor/tunnel goroutines.
func (l *Logbook) Connect(sink func([]Entry) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sink = sink
	dir := filepath.Dir(l.writer.Filename)
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	batch := make([]Entry, 0, 100)
	appendEntry := func(entry Entry) error {
		batch = append(batch, entry)
		if len(batch) < 100 {
			return nil
		}
		if err := sink(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for _, file := range files {
		if !file.Type().IsRegular() || (file.Name() != "capture.log" && !isLogBackup(file.Name())) {
			continue
		}
		if err := replayLogFile(filepath.Join(dir, file.Name()), appendEntry); err != nil {
			return err
		}
	}
	if len(batch) > 0 {
		return sink(batch)
	}
	return nil
}

func replayLogFile(path string, sink func(Entry) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var reader io.Reader = f
	if filepath.Ext(path) == ".gz" {
		z, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer z.Close()
		reader = z
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 5*1024*1024)
	for scanner.Scan() {
		var line struct {
			Time, Level, Msg, Detail, Scope string
		}
		// An interrupted final file write must not prevent intact records importing.
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Time == "" || line.Msg == "" || line.Scope == "backend" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, line.Time); err != nil {
			continue
		}
		if err := sink(Entry{Time: line.Time, Level: line.Level, Message: line.Msg, Detail: line.Detail, Scope: line.Scope}); err != nil {
			return fmt.Errorf("导入历史日志失败：%w", err)
		}
	}
	return scanner.Err()
}
