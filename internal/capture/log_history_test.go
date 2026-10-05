package capture

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryImportsCompressedRotationsInBatchesAndSkipsBrokenLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "logs", "capture-2026-09-30T01-00-00.000.log.gz"))
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	for i := 0; i < 205; i++ {
		fmt.Fprintf(z, `{"time":"2026-09-30T01:00:00Z","level":"INFO","msg":"entry %d","scope":"runtime"}`+"\n", i)
	}
	fmt.Fprintln(z, `{"time":"invalid","msg":"invalid time"}`)
	fmt.Fprintln(z, `{"time":"2026-09-30T01:00:00Z","msg":"already persisted","scope":"backend"}`)
	fmt.Fprintln(z, `{"time":`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "capture-unrelated.log"), []byte(`{"time":"2026-09-30T01:00:00Z","msg":"unrelated"}`), 0600); err != nil {
		t.Fatal(err)
	}
	l := NewLogbook(dir)
	defer l.Close()
	batches := []int{}
	if err := l.Connect(func(entries []Entry) error { batches = append(batches, len(entries)); return nil }); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(batches) != "[100 100 5]" {
		t.Fatal("incorrect or unbounded history batches", batches)
	}
	l.AddBackend("info", "forwarded", "")
	l.AddCertificate("info", "live certificate", "")
	if fmt.Sprint(batches) != "[100 100 5 1]" {
		t.Fatal("live records duplicated or lost", batches)
	}
}
