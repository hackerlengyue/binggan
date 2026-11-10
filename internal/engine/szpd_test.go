package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func szpdTestKey() Key {
	return Key{Passwords: map[string]string{}, Data: map[string]any{"pattern": "szpd", "aesKey": strings.Repeat("ab", 32), "aesIV": strings.Repeat("cd", 16)}}
}

func szpdFixture(t *testing.T, plain []byte) []byte {
	t.Helper()
	k, iv, _ := szpdTestKey().szpdParameters()
	block, _ := aes.NewCipher(k)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	raw := make([]byte, 0x24+len(padded))
	copy(raw, "SZPD")
	binary.LittleEndian.PutUint32(raw[4:], 582)
	binary.LittleEndian.PutUint32(raw[12:], uint32(len(plain)))
	binary.LittleEndian.PutUint32(raw[16:], uint32(len(padded)))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(raw[0x24:], padded)
	return raw
}

func TestSZPDStreamingAndStrictValidation(t *testing.T) {
	key, iv, _ := szpdTestKey().szpdParameters()
	for _, length := range []int{0, 1, 16, (1 << 20) - 1, 1 << 20, (2 << 20) + 17} {
		plain := bytes.Repeat([]byte{0x73}, length)
		raw := szpdFixture(t, plain)
		var out bytes.Buffer
		if err := decryptSZPD582(context.Background(), bytes.NewReader(raw), int64(len(raw)), &out, key, iv, nil); err != nil || !bytes.Equal(plain, out.Bytes()) {
			t.Fatalf("size=%d: %v", length, err)
		}
	}
	base := szpdFixture(t, []byte("synthetic media bytes"))
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:20] },
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], 0); return b },
		func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], 17); return b },
		func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], ^uint32(0)); return b },
		func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:], 16); return b },
		func(b []byte) []byte { b[len(b)-1] ^= 0xff; return b },
	} {
		raw := mutate(bytes.Clone(base))
		if err := decryptSZPD582(context.Background(), bytes.NewReader(raw), int64(len(raw)), &bytes.Buffer{}, key, iv, nil); err == nil {
			t.Fatal("accepted invalid header, truncation or padding")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	raw := szpdFixture(t, bytes.Repeat([]byte{1}, 2<<20))
	err := decryptSZPD582(ctx, bytes.NewReader(raw), int64(len(raw)), &bytes.Buffer{}, key, iv, func(float64, float64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

// NIST SP 800-38A F.2.5 AES-256-CBC first block, followed by a PKCS#7 block.
func TestSZPDAES256KnownBlock(t *testing.T) {
	decode := func(s string) []byte { b, _ := hex.DecodeString(s); return b }
	key := decode("603deb1015ca71be2b73aef0857d77811f352c073b6108d72d9810a30914dff4")
	iv := decode("000102030405060708090a0b0c0d0e0f")
	ct := decode("f58c4c04d6e5f1ba779eabfb5f7bfbd6")
	block, _ := aes.NewCipher(key)
	pad := bytes.Repeat([]byte{16}, 16)
	cipher.NewCBCEncrypter(block, ct).CryptBlocks(pad, pad)
	raw := make([]byte, 0x24)
	copy(raw, "SZPD")
	binary.LittleEndian.PutUint32(raw[4:], 582)
	binary.LittleEndian.PutUint32(raw[12:], 16)
	binary.LittleEndian.PutUint32(raw[16:], 32)
	raw = append(append(raw, ct...), pad...)
	var out bytes.Buffer
	err := decryptSZPD582(context.Background(), bytes.NewReader(raw), int64(len(raw)), &out, key, iv, nil)
	if err != nil || !bytes.Equal(out.Bytes(), decode("6bc1bee22e409f96e93d7e117393172a")) {
		t.Fatalf("NIST block: %x %v", out.Bytes(), err)
	}
}

func TestSZPDFormatPreflight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "课程.sz")
	k := szpdTestKey()
	if err := k.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw  []byte
		key  Key
		want string
	}{
		{szpdFixture(t, []byte("media")), k, ""},
		{szpdFixture(t, []byte("media")), Key{}, "aesKey/aesIV"},
		{[]byte("SZPD"), k, "不完整"},
		{[]byte("SZPD\x46\x01\x00\x00"), k, "326"},
		{[]byte("SZPDF\x02\x00\x00"), k, ""}, // 582 begins SZPDF: F is the low version byte.
		{[]byte("\x00\x00\x00\x18ftypisom"), k, "MP4"},
	} {
		if err := os.WriteFile(path, tc.raw, 0600); err != nil {
			t.Fatal(err)
		}
		err := ValidateInputKey(path, tc.key)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Fatalf("want %q: %v", tc.want, err)
		}
	}
}

func TestSZPDEndToEndRemuxAndFailureCleanup(t *testing.T) {
	toolsAvailable(t)
	dir := t.TempDir()
	media := filepath.Join(dir, "original.mkv")
	ctx := context.Background()
	if err := run(ctx, "ffmpeg", nil, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "0.5", "-c:v", "libx264", "-c:a", "aac", media); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "课程.sz")
	if err := os.WriteFile(input, szpdFixture(t, plain), 0600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "导出")
	if err := os.Mkdir(outDir, 0700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(outDir, "video.mp4")
	var stages []string
	result, err := Run(ctx, input, out, szpdTestKey(), "", nil, func(p Progress) { stages = append(stages, p.Stage) })
	if err != nil || result["format"] != "SZPD" {
		t.Fatalf("pipeline: %v %+v", err, result)
	}
	if len(stages) == 0 || stages[len(stages)-1] != "complete" {
		t.Fatalf("progress: %v", stages)
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) < 8 || string(data[4:8]) != "ftyp" {
		t.Fatalf("not MP4: %v", err)
	}
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	wrong := szpdTestKey()
	wrong.Data["aesKey"] = strings.Repeat("00", 32)
	if _, err := Run(ctx, input, out, wrong, "", nil, nil); err == nil {
		t.Fatal("wrong key accepted")
	}
	// Valid AES padding does not imply valid media. Never publish such a file.
	if err := os.WriteFile(input, szpdFixture(t, []byte("not a video")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, input, out, szpdTestKey(), "", nil, nil); err == nil {
		t.Fatal("invalid media accepted")
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed run retained output: %v %v", entries, err)
	}
}
