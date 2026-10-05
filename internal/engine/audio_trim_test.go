package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Stream-copy seeking creates an MP4 edit list, so the decoder returns a
// shortened first AAC frame. Check both parities against the original sample
// positions, not just whether the encoder accepts the input.
func TestRestoreAudioTrimmedStart(t *testing.T) {
	toolsAvailable(t)
	ctx := context.Background()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.m4a")
	ffmpeg := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command("ffmpeg", append([]string{"-nostdin", "-v", "error", "-y"}, args...)...).Output()
		if err != nil {
			t.Fatalf("ffmpeg: %v", err)
		}
		return out
	}
	ffmpeg("-f", "lavfi", "-i", "sine=frequency=17000:sample_rate=44100:duration=0.3", "-ac", "2", "-c:a", "aac", base)
	for _, skipped := range []int{0, 576, 577} {
		t.Run(fmt.Sprintf("skip_%d", skipped), func(t *testing.T) {
			source := filepath.Join(dir, fmt.Sprintf("trim-%d.m4a", skipped))
			ffmpeg("-ss", fmt.Sprintf("%.12f", float64(skipped)/44100), "-i", base, "-c", "copy", "-movie_timescale", "44100", source)
			scan, _, err := ScanAudio(ctx, source, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var frames []AudioFrame
			err = walkAudio(ctx, source, nil, func(f AudioFrame, _ []byte) error { frames = append(frames, f); return nil })
			if err != nil || len(frames) < 2 || frames[0].Samples != 1024-skipped || frames[len(frames)-1].Samples >= 1024 {
				t.Fatalf("fixture must have expected first frame and short tail: %v %v", frames, err)
			}
			pcm := filepath.Join(dir, fmt.Sprintf("restored-%d.mka", skipped))
			f, err := os.Create(pcm)
			if err != nil {
				t.Fatal(err)
			}
			count, err := writeRestoredAudio(ctx, f, source, 44100, scan, 422, nil)
			closeErr := f.Close()
			if err != nil || closeErr != nil || count != len(frames) {
				t.Fatalf("restore count=%d want=%d error=%v close=%v", count, len(frames), err, closeErr)
			}
			before := ffmpeg("-i", source, "-f", "f32le", "-c:a", "pcm_f32le", "pipe:1")
			after := ffmpeg("-i", pcm, "-f", "f32le", "-c:a", "pcm_f32le", "pipe:1")
			if len(before) != len(after) {
				t.Fatalf("sample bytes changed: %d -> %d", len(before), len(after))
			}
			for off := 0; off < len(before); off += 4 {
				want := binary.LittleEndian.Uint32(before[off:])
				if (off/8+skipped)%2 == 1 {
					want ^= 1 << 31
				}
				if got := binary.LittleEndian.Uint32(after[off:]); got != want {
					t.Fatalf("sample %d channel %d: got %x want %x", off/8, off/4%2, got, want)
				}
			}
			i := 0
			err = walkAudio(ctx, pcm, nil, func(f AudioFrame, _ []byte) error {
				if i >= len(frames) || f.Samples != frames[i].Samples || math.Abs(f.Time-frames[i].Time) > .000002 {
					return fmt.Errorf("frame %d timing or size changed", i)
				}
				i++
				return nil
			})
			if err != nil || i != len(frames) {
				t.Fatalf("frame preservation: %v count=%d", err, i)
			}
		})
	}
}
