package store

import (
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"path/filepath"
)

// Advisory OS locks are released after a crash; stale PID files cannot block restart.
func Lock(dir string) (func() error, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(dir, ".instance.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("数据目录已被另一个后端使用：%s", dir)
	}
	return l.Unlock, nil
}
