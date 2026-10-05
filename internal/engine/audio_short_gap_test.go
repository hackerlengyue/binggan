package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This invalid AAC packet is rejected without the legacy "[aac @"
// diagnostic on FFmpeg 8/9. Recovery must depend on decoder failure, not stderr spelling.
func TestAACRecoveryShortGaps(t *testing.T) {
	toolsAvailable(t)
	bad, err := os.ReadFile("testdata/aac-invalid-packet.bin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		rate, count int
		reject      bool
	}{
		{44100, 1, false}, {44100, 5, false}, {44100, 8, false},
		{48000, 9, false}, {48000, 10, true},
	} {
		t.Run(fmt.Sprintf("%d_%d", tc.rate, tc.count), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "gap.m4a")
			out, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=1000:sample_rate=%d:duration=2", tc.rate), "-ac", "2", "-c:a", "aac", "-b:a", "192k", path).CombinedOutput()
			if err != nil {
				t.Fatalf("fixture: %v %s", err, out)
			}
			before, _, err := ScanAudio(ctx, path, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			samples, err := Samples(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			targets := make(map[int64]bool)
			for _, p := range samples[30 : 30+tc.count] {
				clear(data[p.Offset : p.Offset+int64(p.Size)])
				if p.Size < len(bad) {
					t.Fatal("fixture packet too small")
				}
				copy(data[p.Offset:p.Offset+int64(p.Size)], bad)
				for i := p.Offset; i < p.Offset+int64(p.Size); i++ {
					targets[i] = true
				}
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err = ScanAudio(ctx, path, nil, nil); err == nil {
				t.Fatal("strict scan accepted damaged fixture")
			}
			after, _, err := scanInputAudio(ctx, path, nil, nil)
			patched, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.reject {
				if err == nil {
					t.Fatal("gap over 200 ms accepted")
				}
				if !bytes.Equal(data, patched) {
					t.Fatal("rejected file changed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Concealed) != tc.count || after.Frames != before.Frames || math.Abs(after.End-before.End) > 1e-6 || math.Abs(after.First-before.First) > 1e-6 || len(after.Gaps) != 0 {
				t.Fatalf("timing or recovery changed: before=%+v after=%+v", before, after)
			}
			if len(data) != len(patched) {
				t.Fatal("file size changed")
			}
			for i := range data {
				if data[i] != patched[i] && !targets[int64(i)] {
					t.Fatalf("unrelated byte changed at %d", i)
				}
			}
			if bytes.Equal(data, patched) {
				t.Fatal("damaged packets unchanged")
			}
			pcm, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-xerror", "-i", path, "-f", "f32le", "-c:a", "pcm_f32le", "pipe:1").Output()
			if err != nil {
				t.Fatal(err)
			}
			start := int(math.Round(after.Concealed[0].Time*float64(tc.rate))) * 8
			// AAC overlap may carry the preceding frame into the first replacement;
			// later replacement frames must decode to silence.
			if tc.count > 1 {
				for i := start + 1024*8; i < start+tc.count*1024*8; i += 4 {
					if math.Float32frombits(binary.LittleEndian.Uint32(pcm[i:])) != 0 {
						t.Fatal("replacement did not decode to silence")
					}
				}
			}
		})
	}
}
