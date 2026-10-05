package power

import (
	"errors"
	"testing"
	"time.haomen/binggan/v2/internal/store"
)

func fixture(t *testing.T) (*Controller, *store.Store, *int, *int) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	acquired, released := 0, 0
	c := New(db, func() func() { acquired++; return func() { released++ } })
	c.startSaver = func() (func() error, func(), error) { return func() error { return nil }, func() {}, nil }
	t.Cleanup(c.Close)
	return c, db, &acquired, &released
}
func TestPreferenceLifecycle(t *testing.T) {
	c, db, acquired, released := fixture(t)
	s, err := c.Settings()
	if err != nil || s.KeepAwake {
		t.Fatalf("default: %+v %v", s, err)
	}
	for i := 0; i < 2; i++ {
		if _, err = c.Save(Settings{true}); err != nil {
			t.Fatal(err)
		}
	}
	if *acquired != 1 {
		t.Fatal("duplicate assertion")
	}
	c.Close()
	c.Close()
	if *released != 1 {
		t.Fatal("release must occur exactly once")
	}
	restored := New(db, func() func() { *acquired++; return func() { *released++ } })
	restored.startSaver = c.startSaver
	defer restored.Close()
	if err = restored.Restore(); err != nil {
		t.Fatal(err)
	}
	if *acquired != 2 {
		t.Fatal("saved preference not restored")
	}
	if _, err = restored.Save(Settings{false}); err != nil {
		t.Fatal(err)
	}
	if *released != 2 {
		t.Fatal("disable did not release")
	}
	if _, err = c.Save(Settings{true}); err == nil {
		t.Fatal("closed controller accepted save")
	}
}
func TestNativeFailureDoesNotPersist(t *testing.T) {
	c, _, acquired, _ := fixture(t)
	cleaned := false
	c.startSaver = func() (func() error, func(), error) {
		return func() error { return errors.New("native failure") }, func() { cleaned = true }, nil
	}
	if _, err := c.Save(Settings{true}); err == nil {
		t.Fatal("expected failure")
	}
	s, _ := c.Settings()
	if s.KeepAwake || *acquired != 0 || !cleaned {
		t.Fatal("failed enable changed preference or leaked resources")
	}
}

type failingStore struct{ Store }

func (failingStore) SetSetting(string, any) error { return errors.New("write failed") }
func TestFailedWriteReleasesNewAssertion(t *testing.T) {
	c, _, acquired, released := fixture(t)
	c.store = failingStore{c.store}
	if _, err := c.Save(Settings{true}); err == nil {
		t.Fatal("expected failure")
	}
	if *acquired != 1 || *released != 1 || c.release != nil {
		t.Fatal("failed write leaked assertion")
	}
}

func TestFailedDisableKeepsExistingAssertion(t *testing.T) {
	c, _, acquired, released := fixture(t)
	if _, err := c.Save(Settings{true}); err != nil {
		t.Fatal(err)
	}
	c.store = failingStore{c.store}
	if _, err := c.Save(Settings{false}); err == nil {
		t.Fatal("expected failure")
	}
	if *acquired != 1 || *released != 0 || c.release == nil {
		t.Fatal("failed disable changed native state")
	}
}
