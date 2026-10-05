package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteDurabilityAndLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if unlock2, err := Lock(dir); err == nil {
		unlock2()
		t.Fatal("second process lock accepted")
	}
	if err = unlock(); err != nil {
		t.Fatal(err)
	}
	unlock, err = Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting("test", map[string]string{"value": "persisted"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Log("error", "server", "test", "full details", "request"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var setting map[string]string
	if err = s.GetSetting("test", &setting); err != nil || setting["value"] != "persisted" {
		t.Fatal(setting, err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM system_logs").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	info, _ := os.Stat(filepath.Join(dir, "app.sqlite"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions are not private")
	}
}
