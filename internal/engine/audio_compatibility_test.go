package engine

import (
	"bytes"
	"context"
	"crypto/rc4"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This 23 ms packet reproduces ESC overflow in FFmpeg. The portable recovery
// must report its loss explicitly and retain every sample position. No keys are stored.
func TestAACRecoveryPipeline(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(ctx, "ffmpeg", append([]string{"-nostdin", "-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	base := filepath.Join(dir, "base.aac")
	run("-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100:duration=2", "-ac", "2", "-c:a", "aac", base)
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	expectedFrames := 0
	for off := 0; off < len(data); expectedFrames++ {
		if off+7 > len(data) {
			t.Fatal("short ADTS fixture")
		}
		size := (int(data[off+3]&3) << 11) | (int(data[off+4]) << 3) | int(data[off+5]>>5)
		if size < 7 || off+size > len(data) {
			t.Fatal("bad ADTS length")
		}
		off += size
	}
	bad, err := os.ReadFile("testdata/aac-esc-overflow.aac")
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	for i := 0; i < 30; i++ {
		if offset+7 > len(data) {
			t.Fatal("short ADTS fixture")
		}
		n := (int(data[offset+3]&3) << 11) | (int(data[offset+4]) << 3) | int(data[offset+5]>>5)
		if n < 7 {
			t.Fatal("bad ADTS length")
		}
		offset += n
	}
	n := (int(data[offset+3]&3) << 11) | (int(data[offset+4]) << 3) | int(data[offset+5]>>5)
	replaced := append(append(append([]byte{}, data[:offset]...), bad...), data[offset+n:]...)
	if err = os.WriteFile(base, replaced, 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.mp4")
	// Reuse H.264 packets so this also runs with the Windows LGPL tool bundle,
	// which intentionally does not include the libx264 encoder.
	_, reference, _, _ := mediaFixture(t)
	run("-i", reference, "-f", "aac", "-i", base, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-t", "2.2", source)
	if _, _, err = ScanAudio(ctx, source, nil, nil); err == nil || !strings.Contains(err.Error(), "ESC overflow") {
		t.Fatalf("strict decoder must reject fixture: %v", err)
	}
	broken, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	streamInfo, err := probeAudio(ctx, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	concealed, err := concealAACPackets(ctx, source, *streamInfo, nil)
	if err != nil || len(concealed) != 1 {
		t.Fatalf("concealment: %v %v", concealed, err)
	}
	patched, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	position, end := concealed[0].Offset, concealed[0].Offset+int64(concealed[0].Size)
	if len(patched) != len(broken) || !bytes.Equal(patched[:position], broken[:position]) || !bytes.Equal(patched[end:], broken[end:]) || bytes.Equal(patched[position:end], broken[position:end]) {
		t.Fatal("concealment must change only the identified packet")
	}
	if _, _, err = ScanAudio(ctx, source, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Restore the deliberately broken fixture for the complete encrypted pipeline.
	if err = os.WriteFile(source, broken, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ReadKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	key.Data["audio"] = json.Number("0")
	mima, err := Mima(key.Data, "")
	if err != nil {
		t.Fatal(err)
	}
	den, _ := number(key.Data["den"])
	pmn, _ := number(key.Data["pmn"])
	f, err := os.OpenFile(source, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	samples, err := Samples(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		b := make([]byte, s.Size)
		if _, err = f.ReadAt(b, s.Offset); err != nil {
			t.Fatal(err)
		}
		cipher, err := rc4.NewCipher(packetKey(Transform(mima), key.Passwords[strconv.FormatInt(segment(s.Offset, den, pmn), 10)], s.Size))
		if err != nil {
			t.Fatal(err)
		}
		cipher.XORKeyStream(b, b)
		if _, err = f.WriteAt(b, s.Offset); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	dest := filepath.Join(dir, "output.mp4")
	result, err := Run(ctx, source, dest, key, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := result["audio_restore"].(map[string]any)["before"].(Scan)
	if len(before.Concealed) != 1 || before.Frames != expectedFrames || len(before.Gaps) != 0 {
		t.Fatalf("incomplete fallback scan: %+v", before)
	}
	if result["check"].(map[string]any)["valid"] != true {
		t.Fatal("output not validated")
	}
	run("-xerror", "-i", dest, "-map", "0:v", "-map", "0:a", "-f", "null", "-")
}

func TestAACRecoveryRejectsBroadDamage(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.m4a")
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100:duration=3", "-ac", "2", "-c:a", "aac", base).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	original, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		indices []int
	}{{"consecutive_over_limit", []int{30, 31, 32, 33, 34, 35, 36, 37, 38}}, {"too_many", []int{10, 13, 16, 19, 22, 25, 28, 31, 34, 37, 40}}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".m4a")
			data := append([]byte{}, original...)
			samples, e := Samples(bytes.NewReader(data), int64(len(data)))
			if e != nil {
				t.Fatal(e)
			}
			for _, i := range tc.indices {
				p := samples[i]
				for j := 0; j < p.Size; j++ {
					data[int(p.Offset)+j] = 255
				}
			}
			if e = os.WriteFile(path, data, 0600); e != nil {
				t.Fatal(e)
			}
			if _, _, e = scanInputAudio(ctx, path, nil, nil); e == nil {
				t.Fatal("broad corruption accepted")
			}
			after, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(data, after) {
				t.Fatal("rejected input was modified")
			}
		})
	}
}

func TestAACSilencePaddingDecodes(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	silent, err := silentAACPacket(ctx, "44100")
	if err != nil {
		t.Fatal(err)
	}
	var stream []byte
	count := 0
	sizes := []int{len(silent), len(silent) + 7, len(silent) + 8, 125, 126, 127, 200, 871, 1500, 2167, 2168, 4334, 6501, 8184}
	for _, size := range sizes {
		packet, e := padAACSilence(silent, size)
		if e != nil {
			t.Fatalf("size=%d: %v", size, e)
		}
		if len(packet) != size {
			t.Fatalf("size=%d got %d", size, len(packet))
		}
		n := size + 7
		stream = append(stream, 255, 241, 80, 128|byte(n>>11), byte(n>>3), byte((n&7)<<5)|31, 252)
		stream = append(stream, packet...)
		count++
	}
	path := filepath.Join(t.TempDir(), "padding.aac")
	if err = os.WriteFile(path, stream, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-f", "aac", "-i", path, "-f", "f32le", "-c:a", "pcm_f32le", "-")
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	pcm, err := cmd.Output()
	if err != nil {
		t.Fatalf("padded AAC rejected: %v %s", err, diagnostics.String())
	}
	if len(pcm) != count*1024*2*4 {
		t.Fatalf("lost frames: bytes=%d expected=%d", len(pcm), count*8192)
	}
	for _, b := range pcm {
		if b != 0 {
			t.Fatal("replacement is not silence")
		}
	}
	for _, size := range []int{-1, 0, len(silent) - 1, len(silent) + 1, 8192} {
		if _, err = padAACSilence(silent, size); err == nil {
			t.Fatalf("invalid size %d accepted", size)
		}
	}
}

// Constant opposite-sign channels expose mute-edge discontinuities without
// depending on AAC encoder noise. Chunk boundaries must not affect the result.
func TestAACConcealmentSmoothEdges(t *testing.T) {
	const rate, samples = 48000, 4800
	original := make([]byte, samples*8)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint32(original[i*8:], math.Float32bits(0.8))
		binary.LittleEndian.PutUint32(original[i*8+4:], math.Float32bits(-0.4))
	}
	gaps := []AACConcealment{{Time: 0.04, Duration: 1024.0 / rate}}
	whole := append([]byte(nil), original...)
	smoothConcealedAudio(whole, AudioFrame{Time: 0, Samples: samples, Channels: 2}, rate, gaps)
	chunked := append([]byte(nil), original...)
	for offset := 0; offset < samples; {
		n := min(317, samples-offset)
		smoothConcealedAudio(chunked[offset*8:(offset+n)*8], AudioFrame{Time: float64(offset) / rate, Samples: n, Channels: 2}, rate, gaps)
		offset += n
	}
	if !bytes.Equal(whole, chunked) {
		t.Fatal("smoothing depends on frame boundaries")
	}
	changed := false
	for i := 0; i < samples; i++ {
		tm := float64(i) / rate
		if tm < 0.035 || tm > 0.04+1024.0/rate+0.005 {
			if !bytes.Equal(whole[i*8:i*8+8], original[i*8:i*8+8]) {
				t.Fatal("changed samples outside repair window")
			}
		}
		l := math.Float32frombits(binary.LittleEndian.Uint32(whole[i*8:]))
		r := math.Float32frombits(binary.LittleEndian.Uint32(whole[i*8+4:]))
		if l < 0 || l > 0.8 || r != -l/2 {
			t.Fatal("channel balance or amplitude changed")
		}
		if tm >= 0.04 && tm <= 0.04+1024.0/rate && (l != 0 || r != 0) {
			t.Fatal("damaged interval not muted")
		}
		if i > 0 {
			prev := math.Float32frombits(binary.LittleEndian.Uint32(whole[(i-1)*8:]))
			if math.Abs(float64(l-prev)) > 0.006 {
				t.Fatal("abrupt mute edge")
			}
		}
		changed = changed || l != 0.8
	}
	if !changed {
		t.Fatal("repair envelope was not applied")
	}
}
