package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func szpdDerivedTestKey() Key {
	return Key{Passwords: map[string]string{}, Data: map[string]any{"pattern": "szpd", "derivation": szpdFilenameFillDerivation}}
}

// Expected values were independently calculated with Python hashlib; the first
// key/IV also match the follow-up report's derive_v2.py example for lesson01.sz.
func TestSZPDDerivationVectors(t *testing.T) {
	salt, _ := hex.DecodeString("a0a1a2a3a4a5a6a7a8a9aaabacadaeaf")
	for _, tc := range []struct {
		nameHex string
		fill    byte
		keyHex  string
	}{
		{"6c6573736f6e30312e737a", 0, "0131b434f03922069d89edda14b839ca9e57a92e2d96d255649968fb1ea32962"},
		{"20e8afbee7a88b2030312e535a", 255, "1b05906edaa59e56970467241f3c48ab647385334833526fa705023ec1acc71c"},
		{"bfceb3cc2e737a", 127, "ef5a93e05c402edde320172f3bec54d5cbfdd0a7f1c88220d6a3af69aa58a0be"},
	} {
		name, _ := hex.DecodeString(tc.nameHex)
		key, iv := deriveSZPD582(name, salt, tc.fill)
		if hex.EncodeToString(key) != tc.keyHex || hex.EncodeToString(iv) != "26935c2e70fda1a1fb82d2ce59bc02fe" {
			t.Fatal("derivation vector mismatch", tc.nameHex)
		}
	}
}

func szpdDerivedFixture(t *testing.T, plain, name []byte, fill byte) []byte {
	t.Helper()
	salt, _ := hex.DecodeString("a0a1a2a3a4a5a6a7a8a9aaabacadaeaf")
	key, iv := deriveSZPD582(name, salt, fill)
	block, _ := aes.NewCipher(key)
	padding := 16 - len(plain)%16
	padded := append(bytes.Clone(plain), bytes.Repeat([]byte{byte(padding)}, padding)...)
	raw := make([]byte, 0x24+len(padded))
	copy(raw, "SZPD")
	binary.LittleEndian.PutUint32(raw[4:], 582)
	binary.LittleEndian.PutUint32(raw[12:], uint32(len(plain)))
	binary.LittleEndian.PutUint32(raw[16:], uint32(len(padded)))
	copy(raw[20:36], salt)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(raw[36:], padded)
	return raw
}

type countedSZPDReader struct {
	io.ReaderAt
	bytes int
}

func (r *countedSZPDReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, offset)
	r.bytes += n
	return n, err
}

func TestSZPDDerivationRecoveryBoundedReadsAndNoFalseSuccess(t *testing.T) {
	name := []byte(" 课程 01.SZ")
	plain := make([]byte, 2<<20)
	copy(plain, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'i', 's', 'o', '2'})
	raw := szpdDerivedFixture(t, plain, name, 255)
	source := &countedSZPDReader{ReaderAt: bytes.NewReader(raw)}
	k := szpdDerivedTestKey().WithInputName(string(name))
	attempts := 0
	key, iv, err := recoverSZPD582Parameters(t.Context(), source, int64(len(raw)), k, func(done, total float64) {
		attempts++
		if total != 256 {
			t.Fatal(total)
		}
	})
	if err != nil || attempts != 256 || source.bytes > 580 {
		t.Fatalf("recovery err=%v attempts=%d read=%d", err, attempts, source.bytes)
	}
	var out bytes.Buffer
	if err := decryptSZPD582(t.Context(), bytes.NewReader(raw), int64(len(raw)), &out, key, iv, nil); err != nil || !bytes.Equal(plain, out.Bytes()) {
		t.Fatalf("recovered wrong parameters: %v", err)
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], szpdDerivedFixture(t, []byte("not media, despite valid AES padding"), name, 255)} {
		if _, _, err := recoverSZPD582Parameters(t.Context(), bytes.NewReader(bad), int64(len(bad)), k, nil); err == nil {
			t.Fatal("invalid source passed recovery")
		}
	}
	wrong := szpdDerivedTestKey().WithInputName(strings.TrimSpace(string(name)))
	if _, _, err := recoverSZPD582Parameters(t.Context(), bytes.NewReader(raw), int64(len(raw)), wrong, nil); err == nil {
		t.Fatal("filename whitespace was ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, _, err = recoverSZPD582Parameters(ctx, bytes.NewReader(raw), int64(len(raw)), k, func(float64, float64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSZPDDerivationNameBindingAndValidation(t *testing.T) {
	k := szpdDerivedTestKey()
	a, b := k.WithInputName("A.sz"), k.WithInputName("B.sz")
	if k.Data["fileName"] != nil || a.Data["fileName"] != "A.sz" || b.Data["fileName"] != "B.sz" {
		t.Fatal("shared batch key mutated")
	}
	if a.WithInputName("input.sz").Data["fileName"] != "A.sz" {
		t.Fatal("original name replaced")
	}
	if err := k.Validate(); err != nil {
		t.Fatal(err)
	}
	hexKey := szpdDerivedTestKey()
	hexKey.Data["fileNameHex"] = "bfceb3cc2e737a"
	rawName, err := hexKey.WithInputName("ignored.sz").szpdFileNameBytes()
	if err != nil || hex.EncodeToString(rawName) != "bfceb3cc2e737a" {
		t.Fatal("exact legacy filename bytes changed")
	}
	for _, fields := range []map[string]any{
		{"derivation": "other"}, {"aesKey": ""}, {"aesIV": ""},
		{"fileName": ""}, {"fileName": "a\x00.sz"}, {"fileName": 123},
		{"fileNameHex": "a"}, {"fileNameHex": "zz"}, {"fileNameHex": "00"},
		{"fileName": "a.sz", "fileNameHex": "612e737a"},
	} {
		invalid := szpdDerivedTestKey()
		for name, value := range fields {
			invalid.Data[name] = value
		}
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid parameters accepted: %v", fields)
		}
	}
}

func TestSZPDDerivationFullMediaValidation(t *testing.T) {
	toolsAvailable(t)
	dir := t.TempDir()
	media := filepath.Join(dir, "plain.mp4")
	if err := run(t.Context(), "ffmpeg", nil, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-t", "0.5", "-c:v", "libx264", media); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	input, outputPath := filepath.Join(dir, "input.sz"), filepath.Join(dir, "output.mp4")
	name := []byte(" 课程 01.SZ")
	if err := os.WriteFile(input, szpdDerivedFixture(t, plain, name, 255), 0600); err != nil {
		t.Fatal(err)
	}
	var stages []string
	k := szpdDerivedTestKey().WithInputName(string(name))
	result, err := Run(t.Context(), input, outputPath, k, "", nil, func(p Progress) { stages = append(stages, p.Stage) })
	if err != nil || result["key_source"] != szpdFilenameFillDerivation {
		t.Fatalf("derived pipeline: %v %v", err, result)
	}
	if len(stages) == 0 || stages[0] != "derive" || stages[len(stages)-1] != "complete" {
		t.Fatal(stages)
	}
	if err := os.Remove(outputPath); err != nil {
		t.Fatal(err)
	}
	// A plausible ftyp box plus valid padding is only a candidate, not a video.
	fake := make([]byte, 128)
	copy(fake, plain[:min(len(plain), 24)])
	if err := os.WriteFile(input, szpdDerivedFixture(t, fake, name, 255), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), input, outputPath, k, "", nil, nil); err == nil {
		t.Fatal("media magic bypassed full validation")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatal("invalid media was published", err)
	}
}
