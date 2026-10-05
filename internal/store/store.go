// Package store persists application state in SQLite. Writers are serialized by
// database/sql; WAL lets readers use a consistent snapshot while work is running.
package store

import (
	"database/sql"
	"github.com/google/uuid"

	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Store struct{ DB *sql.DB }

func ID() string  { return uuid.NewString() }
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "app.sqlite")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(p, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(q); err != nil {
			return fail(err)
		}
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 2 {
		return fail(fmt.Errorf("数据库版本 %d 高于程序支持版本", version))
	}
	tx, err := db.Begin()
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL) STRICT;
 CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY,payload TEXT NOT NULL) STRICT;
 CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,payload TEXT NOT NULL) STRICT;
 CREATE INDEX IF NOT EXISTS jobs_task ON jobs(task_id);
 CREATE TABLE IF NOT EXISTS job_logs(job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,byte_start INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(job_id,byte_start)) STRICT;
 CREATE TABLE IF NOT EXISTS file_cleanup(job_id TEXT PRIMARY KEY) STRICT;
 CREATE TABLE IF NOT EXISTS captures(seq INTEGER PRIMARY KEY,payload TEXT NOT NULL) STRICT;
 CREATE TABLE IF NOT EXISTS system_logs(id TEXT PRIMARY KEY,at TEXT NOT NULL,level TEXT NOT NULL,source TEXT NOT NULL,message TEXT NOT NULL,detail TEXT NOT NULL,request_id TEXT NOT NULL) STRICT;
 CREATE INDEX IF NOT EXISTS system_logs_at ON system_logs(at);
 CREATE INDEX IF NOT EXISTS system_logs_filter ON system_logs(source,level,at);
 CREATE TABLE IF NOT EXISTS resources(id TEXT PRIMARY KEY,name TEXT NOT NULL,task_id TEXT NOT NULL,task_name TEXT NOT NULL,size INTEGER NOT NULL,finished_at TEXT NOT NULL) STRICT;
 CREATE INDEX IF NOT EXISTS resources_finished ON resources(finished_at DESC,id);
 PRAGMA user_version=2;`)
	if err != nil {
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) GetSetting(key string, v any) error {
	var raw string
	err := s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}
func (s *Store) SetSetting(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(raw))
	return err
}
func Encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type Log struct {
	ID        string `json:"id"`
	At        string `json:"at"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	RequestID string `json:"requestId"`
}

func (s *Store) Log(level, source, message, detail, requestID string) error {
	return s.InsertLog(Log{ID: ID(), At: Now(), Level: level, Source: source, Message: message, Detail: detail, RequestID: requestID})
}
func (s *Store) InsertLog(l Log) error {
	_, err := s.DB.Exec("INSERT OR IGNORE INTO system_logs VALUES(?,?,?,?,?,?,?)", l.ID, l.At, l.Level, l.Source, l.Message, l.Detail, l.RequestID)
	return err
}
func (s *Store) AppendJobLog(id, level, message string) error {
	b := []byte(fmt.Sprintf("[%s] [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, message))
	_, err := s.DB.Exec("INSERT INTO job_logs(job_id,byte_start,data) SELECT ?,COALESCE(MAX(byte_start+length(data)),0),? FROM job_logs WHERE job_id=?", id, b, id)
	return err
}
