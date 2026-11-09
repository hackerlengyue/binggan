package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPlayer261056ReferenceHash(t *testing.T) {
	sum := md5.Sum([]byte("801f49182b6b1f6c1d64d838feea1fcd207631a3561c27ef581599573facc743"))
	want := hex.EncodeToString(sum[:])
	got, err := ResolveHash(map[string]any{"softwareName": "MAC_sz_26.10.56"}, "", nil)
	if err != nil || got != want || KnownPlayers["MAC_sz_26.10.56"] != want {
		t.Fatalf("new reference hash: %q, %v; want %q", got, err, want)
	}
}

func TestPlayer261056PacketKeys(t *testing.T) {
	master := "3" + strings.Repeat("a", 63)
	for _, mode := range []string{"1", "3", "<null>", "4"} {
		k := Key{Data: map[string]any{"pattern": mode, "whenLong": 10, "den": 999, "pmn": 2, "mimatuokeqian": "direct-key"}, Passwords: map[string]string{"2": "fixed-slot", "4": "offset-slot"}}
		got, err := sampleKey(k, master, 60, 123)
		want := map[string]string{"1": master[:10] + "123fixed-slot", "3": master[:10] + "123offset-slo", "<null>": master + "123", "4": "direct-key"}[mode]
		if err != nil || string(got) != want {
			t.Errorf("mode %s: %q (%v), want %q", mode, got, err, want)
		}
	}
	k := Key{Data: map[string]any{"pattern": "3", "den": 10, "pmn": 2}, Passwords: map[string]string{"1": "key"}}
	if _, err := sampleKey(k, master, 60, 123); err == nil {
		t.Fatal("missing slot accepted")
	}
}

func TestPlayer261056AESPaddingAndMode(t *testing.T) {
	// OpenSSL AES-256-ECB vector is frozen separately from Go's encryptor.
	data := map[string]any{"softwareName": "MAC_sz_26.10.56", "md51": strings.Repeat("a", 32), "md52": strings.Repeat("b", 32), "md53": strings.Repeat("c", 32)}
	appHash := KnownPlayers["MAC_sz_26.10.56"]
	data["mimaEnc"] = "WyNJJaeBMgvTk0v9nlPm7h3Tef/+Xq4HOwCLP2zsnfsd03n//l6uBzsAiz9s7J37HdN5//5ergc7AIs/bOyd+xMAI7GENxccXJNbC1+RRh4="
	want := "3" + strings.Repeat("a", 63)
	if got, err := Mima(data, appHash); err != nil || got != want {
		t.Fatalf("OpenSSL ECB vector: %q, %v", got, err)
	}
	data["mimaEnc"] = "WyNJJaeBMgvTk0v9nlPm7g5YloI9EcivxsC6TAtEOxq1ysp+vYorEJ4Eau6u2O33N+f2a+w+c9L335vy2Fym0e49VuMbTOGui6NwrKeP24s="
	if _, err := Mima(data, appHash); err == nil {
		t.Fatal("new player accepted CBC ciphertext")
	}

	sum := md5.Sum([]byte(appHash + strings.Repeat("a", 32) + strings.Repeat("b", 32) + strings.Repeat("c", 32)))
	block, err := aes.NewCipher([]byte(hex.EncodeToString(sum[:])))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(strings.Repeat("a", 32)) // A hex-looking plaintext with no PKCS#7 padding.
	ct := make([]byte, len(plain))
	for i := 0; i < len(ct); i += 16 {
		block.Encrypt(ct[i:i+16], plain[i:i+16])
	}
	data["mimaEnc"] = base64.StdEncoding.EncodeToString(ct)
	if _, err := Mima(data, appHash); err == nil {
		t.Fatal("invalid padding accepted")
	}
}

func TestPlayer261056FullDecryptModes(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dir := t.TempDir()
	clearPath := filepath.Join(dir, "clear.mp4")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=6:duration=1", "-c:v", "libx264", "-an", clearPath)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, b)
	}
	clear, err := os.ReadFile(clearPath)
	if err != nil {
		t.Fatal(err)
	}
	samples, err := Samples(bytes.NewReader(clear), int64(len(clear)))
	if err != nil || len(samples) == 0 {
		t.Fatalf("samples: %v", err)
	}
	master := "3" + strings.Repeat("a", 63)
	for _, mode := range []string{"1", "3", "<null>", "4"} {
		t.Run(mode, func(t *testing.T) {
			k := Key{Data: map[string]any{"softwareName": "MAC_sz_26.10.56", "pattern": mode, "mima": master, "whenLong": 100, "den": 1, "pmn": 7, "audio": 0, "mimatuokeqian": "direct-test-key"}, Passwords: map[string]string{}}
			encrypted := bytes.Clone(clear)
			seen := map[int]bool{}
			for _, sample := range samples {
				// Independent slot and key formulas from the analysis, not sampleKey.
				slot := 7
				if mode == "3" {
					slot = min(180, int(math.Abs(math.Trunc(math.RoundToEven(float64(sample.Offset)*.01)/7)))+1)
				}
				seen[slot] = true
				pwd := fmt.Sprintf("%010d%022d", slot, slot)
				k.Passwords[strconv.Itoa(slot)] = pwd
				packetKey := master[:10] + strconv.Itoa(sample.Size) + pwd[:10]
				if mode == "<null>" {
					packetKey = master + strconv.Itoa(sample.Size)
				}
				if mode == "4" {
					packetKey = "direct-test-key"
				}
				cipher, err := rc4.NewCipher([]byte(packetKey))
				if err != nil {
					t.Fatal(err)
				}
				packet := encrypted[sample.Offset : sample.Offset+int64(sample.Size)]
				cipher.XORKeyStream(packet, packet)
			}
			if mode == "3" && len(seen) < 2 {
				t.Fatal("fixture does not span multiple slots")
			}
			if mode == "<null>" || mode == "4" {
				k.Passwords = nil
				delete(k.Data, "whenLong")
				delete(k.Data, "den")
				delete(k.Data, "pmn")
			}
			if mode == "4" {
				delete(k.Data, "mima")
			}
			input, output := filepath.Join(t.TempDir(), "input.sz"), filepath.Join(t.TempDir(), "output.mp4")
			if err := os.WriteFile(input, encrypted, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Run(ctx, input, output, k, "", nil, nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, clear) {
				t.Fatalf("mode %s changed decrypted bytes (%v)", mode, err)
			}
		})
	}
}

func TestSlotRoundTiesToEven(t *testing.T) {
	for _, tc := range []struct{ pos, den, pmn, want int64 }{
		{5, 10, 1, 1}, {15, 10, 1, 3}, {25, 10, 1, 3},
		{24, 10, 1, 3}, {26, 10, 1, 4}, {99999, 10, 1, 180},
	} {
		if got := segment(tc.pos, tc.den, tc.pmn); got != tc.want {
			t.Errorf("slot(%d,%d,%d)=%d, want %d", tc.pos, tc.den, tc.pmn, got, tc.want)
		}
	}
}

func TestPlayer261056ModesAndWhenLong(t *testing.T) {
	for _, mode := range []string{"1", "3", "<null>", "4"} {
		k := Key{Data: map[string]any{"softwareName": "MAC_sz_26.10.56", "pattern": mode, "mima": "3aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
		if mode == "1" || mode == "3" {
			k.Data["whenLong"], k.Data["pmn"] = 10, 7
			k.Passwords = map[string]string{"7": "0123456789abcdef0123456789abcdef"}
		}
		if mode == "4" {
			k.Data["mimatuokeqian"] = "direct-rc4-key"
		}
		if err := k.Validate(); err != nil {
			t.Errorf("mode %s rejected: %v", mode, err)
		}
	}
	for _, mode := range []any{"null", "", nil, "2", "4"} {
		k := Key{Data: map[string]any{"pattern": mode, "mima": "3aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "den": 10, "pmn": 1}, Passwords: map[string]string{"1": "key"}}
		if err := k.Validate(); err == nil {
			t.Errorf("invalid mode/direct key accepted: %v", mode)
		}
	}
}
