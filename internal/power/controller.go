// Package power owns the persistent, application-lifetime display preference.
package power

import (
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type Settings struct {
	KeepAwake bool `json:"keepAwake"`
}
type Store interface {
	GetSetting(string, any) error
	SetSetting(string, any) error
}

// Controller methods are serialized by the desktop main thread. The pulse
// worker never calls MyGo, so stopping it during application shutdown is safe.
type Controller struct {
	store      Store
	acquire    func() func()
	startSaver func() (func() error, func(), error)
	release    func()
	closed     bool
}

func New(store Store, acquire func() func()) *Controller {
	return &Controller{store: store, acquire: acquire, startSaver: startScreenSaverGuard}
}
func (c *Controller) Settings() (Settings, error) {
	var s Settings
	err := c.store.GetSetting("power", &s)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return s, err
}
func (c *Controller) Restore() error {
	s, err := c.Settings()
	if err != nil {
		return err
	}
	return c.apply(s.KeepAwake)
}
func (c *Controller) Save(s Settings) (Settings, error) {
	if c.closed {
		return Settings{}, errors.New("应用正在退出")
	}
	old, err := c.Settings()
	if err != nil {
		return Settings{}, err
	}
	wasActive := c.release != nil
	// Persist disabling before releasing, so a failed write keeps the
	// previously active assertion without a risky native reacquisition.
	if s.KeepAwake {
		if err = c.apply(true); err != nil {
			return old, err
		}
	}
	if err = c.store.SetSetting("power", s); err != nil {
		if !wasActive {
			_ = c.apply(false)
		}
		return old, err
	}
	if !s.KeepAwake {
		_ = c.apply(false)
	}
	return s, nil
}
func (c *Controller) apply(enabled bool) error {
	if c.closed {
		return errors.New("应用正在退出")
	}
	if !enabled {
		if c.release != nil {
			c.release()
			c.release = nil
		}
		return nil
	}
	if c.release != nil {
		return nil
	}
	pulse, cleanup, err := c.startSaver()
	if err != nil {
		return err
	}
	if err = pulse(); err != nil {
		cleanup()
		return err
	}
	releaseDisplay := c.acquire()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var reported bool
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := pulse(); err != nil {
					if !reported {
						slog.Warn("阻止屏保失败", "error", err)
						reported = true
					}
				} else {
					reported = false
				}
			}
		}
	}()
	var once sync.Once
	c.release = func() { once.Do(func() { close(stop); <-done; cleanup(); releaseDisplay() }) }
	return nil
}
func (c *Controller) Close() {
	if c.release != nil {
		c.release()
		c.release = nil
	}
	c.closed = true
}
