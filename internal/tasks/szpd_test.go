package tasks

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/store"
)

func TestSZPDDerivationOriginalNamesSurviveBatchRetentionAndRestartRetry(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(root, "plain.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-t", "0.5", "-c:v", "libx264", media).CombinedOutput(); err != nil {
		t.Fatalf("generate media: %v %s", err, out)
	}
	plain, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Close() }()
	if _, err := s.DB.Exec("CREATE TRIGGER fail_start BEFORE UPDATE ON jobs WHEN json_extract(NEW.payload,'$.status')='running' BEGIN SELECT RAISE(FAIL,'temporary fault'); END"); err != nil {
		t.Fatal(err)
	}
	k, err := engine.ReadKey([]byte(`{"passwords":{},"getPwdData":{"pattern":"szpd","derivation":"filename-fill-v1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var inputs []Prepared
	var originalPaths []string
	// Independently calculated Python hashlib vectors, not the engine's KDF.
	for i, tc := range []struct{ name, key string }{
		{"lesson01.sz", "0131b434f03922069d89edda14b839ca9e57a92e2d96d255649968fb1ea32962"},
		{" 课程 01.SZ", "1b05906edaa59e56970467241f3c48ab647385334833526fa705023ec1acc71c"},
	} {
		key, _ := hex.DecodeString(tc.key)
		iv, _ := hex.DecodeString("26935c2e70fda1a1fb82d2ce59bc02fe")
		block, _ := aes.NewCipher(key)
		padding := 16 - len(plain)%16
		raw := make([]byte, 36+len(plain)+padding)
		copy(raw, "SZPD")
		binary.LittleEndian.PutUint32(raw[4:], 582)
		binary.LittleEndian.PutUint32(raw[12:], uint32(len(plain)))
		binary.LittleEndian.PutUint32(raw[16:], uint32(len(plain)+padding))
		for j := 0; j < 16; j++ {
			raw[20+j] = byte(0xa0 + j)
		}
		copy(raw[36:], plain)
		for j := 36 + len(plain); j < len(raw); j++ {
			raw[j] = byte(padding)
		}
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(raw[36:], raw[36:])
		path := filepath.Join(root, []string{"upload-a.sz", "upload-b.sz"}[i])
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, Prepared{Input: path, Name: tc.name, Key: k})
		originalPaths = append(originalPaths, path)
	}
	view, err := m.Create("filename derivation", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if k.Data["fileName"] != nil {
		t.Fatal("batch changed the imported key")
	}
	for _, child := range view.Children {
		if job := waitTerminal(t, m, child.ID); job.Status != "failed" {
			t.Fatalf("expected injected startup failure: %+v", job)
		}
		p, err := m.readPrepared(child.ID)
		if err != nil || p.Key.Data["fileName"] != child.Name || filepath.Base(p.Input) != "input.sz" {
			t.Fatalf("original filename lost: %+v %v", p, err)
		}
	}
	m.Close()
	if _, err := s.DB.Exec("DROP TRIGGER fail_start"); err != nil {
		t.Fatal(err)
	}
	for _, path := range originalPaths {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	m, err = New(s, root)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{view.Children[0].ID, view.Children[1].ID}
	if _, err := m.Retry(view.ID, ids); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		job := waitTerminal(t, m, id)
		result, ok := job.Result.(map[string]any)
		if job.Status != "completed" || job.Attempt != 2 || !ok || result["key_source"] != "filename-fill-v1" {
			t.Fatalf("retry could not derive with original name: %+v", job)
		}
	}
}
