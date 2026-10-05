package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func toolsAvailable(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("integration test requires %s: %v", name, err)
		}
	}
}
func TestLegacyAESVector(t *testing.T) {
	b, err := os.ReadFile("testdata/key-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	json.Unmarshal(b, &v)
	m, err := Mima(map[string]any{"mimaEnc": v["encrypted"], "md51": v["md51"], "md52": v["md52"], "md53": v["md53"]}, v["appMd5"])
	if err != nil || m != v["mima"] {
		t.Fatalf("legacy AES mismatch: %v", err)
	}
	if Transform(m) != v["transformed"] {
		t.Fatal("legacy key transform mismatch")
	}
	if _, err = Mima(map[string]any{"mimaEnc": v["encrypted"], "md51": v["md51"], "md52": v["md52"], "md53": v["md53"]}, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"); err == nil {
		t.Fatal("wrong App hash accepted")
	}
}
func TestGoldenDecryptAndAudio(t *testing.T) {
	toolsAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	b, _ := os.ReadFile("testdata/key.json")
	k, err := ReadKey(b)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "output.mp4")
	var stages []string
	result, err := Run(ctx, "testdata/encrypted.sz", out, k, "", func(level, msg string) {
		if level == "STDERR" {
			t.Log(msg)
		}
	}, func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["check"].(map[string]any)["valid"] != true {
		t.Fatal("invalid output")
	}
	scan, _, err := ScanAudio(ctx, out, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scan.High != 0 || len(scan.Gaps) > 0 {
		t.Fatalf("unrestored audio: %+v", scan)
	}
	var reference struct {
		Packets []struct {
			Hash string  `json:"hash"`
			PTS  float64 `json:"pts"`
			DTS  float64 `json:"dts"`
		} `json:"video_packets"`
		Audio Scan `json:"audio"`
	}
	b, _ = os.ReadFile("testdata/reference.json")
	if err = json.Unmarshal(b, &reference); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v", "-show_packets", "-show_data_hash", "sha256", "-show_entries", "packet=pts_time,dts_time,data_hash", "-of", "json", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Packets []struct {
			Hash string `json:"data_hash"`
			PTS  string `json:"pts_time"`
			DTS  string `json:"dts_time"`
		} `json:"packets"`
	}
	json.Unmarshal(raw, &actual)
	if len(reference.Packets) != len(actual.Packets) {
		t.Fatal("video packet count changed")
	}
	for i, p := range actual.Packets {
		r := reference.Packets[i]
		pts, _ := strconv.ParseFloat(p.PTS, 64)
		dts, _ := strconv.ParseFloat(p.DTS, 64)
		if p.Hash != r.Hash || math.Abs(pts-r.PTS) > .00002 || math.Abs(dts-r.DTS) > .00002 {
			t.Fatalf("video packet %d changed: pts %f vs %f dts %f vs %f", i, pts, r.PTS, dts, r.DTS)
		}
	}
	if math.Abs(scan.End-reference.Audio.End) > .1 {
		t.Fatal("audio duration changed")
	}
	t.Logf("stages=%v, audio frames=%d, verified video packets=%d", stages, scan.Frames, len(actual.Packets))
}
func TestInvalidAndCancelledInput(t *testing.T) {
	b, _ := os.ReadFile("testdata/key.json")
	k, _ := ReadKey(b)
	out := filepath.Join(t.TempDir(), "output.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "testdata/encrypted.sz", out, k, "", nil, nil); err == nil {
		t.Fatal("cancelled decryption succeeded")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("cancelled work published a file")
	}
	if _, err := Samples(bytes.NewReader([]byte("invalid")), 7); err == nil {
		t.Fatal("invalid MP4 accepted")
	}
}
func TestAudioIntervalValidation(t *testing.T) {
	s := Scan{}
	for i := 0; i < 12; i++ {
		v := Second{Second: i, RMS: .1, HighFraction: .001}
		if i/4%2 == 0 {
			v.HighFraction = .999
			s.High++
		}
		s.Seconds = append(s.Seconds, v)
	}
	n, _, err := ChooseInterval(Key{Data: map[string]any{}}, s)
	if n != 4 || err != nil {
		t.Fatal(n, err)
	}
	for _, bad := range []any{-1, true, 2.5, "bad", 3} {
		if _, _, err = ChooseInterval(Key{Data: map[string]any{"audio": bad}}, s); err == nil {
			t.Fatalf("invalid interval accepted: %v", bad)
		}
	}
}
func TestRealVideo(t *testing.T) {
	input := os.Getenv("SZJM_TEST_INPUT")
	if input == "" {
		t.Skip("optional local video fixture")
	}
	toolsAvailable(t)
	b, err := os.ReadFile(os.Getenv("SZJM_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	k, err := ReadKey(b)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := ResolveHash(k.Data, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("SZJM_TEST_OUTPUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "real.mp4")
	}
	lastStage := ""
	stageStart := time.Now()
	result, err := Run(context.Background(), input, out, k, hash, func(level, msg string) {
		if level == "STDERR" {
			t.Log(msg)
		}
	}, func(p Progress) {
		if p.Stage != lastStage {
			if lastStage != "" {
				t.Logf("stage %s: %.3f s", lastStage, time.Since(stageStart).Seconds())
			}
			lastStage, stageStart = p.Stage, time.Now()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real video passed: engine=%s elapsed=%v samples=%v", result["engine"], result["elapsed_sec"], result["samples"])
}

func TestPCMContainerPreservesFrameGaps(t *testing.T) {
	toolsAvailable(t)
	path := filepath.Join(t.TempDir(), "gap.mka")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = pcmHeader(f, 44100); err != nil {
		t.Fatal(err)
	}
	timestamps := []float64{0, 1024.0 / 44100, 0.120317}
	frame := make([]byte, 1024*2*4)
	for _, ts := range timestamps {
		if err = pcmFrame(f, ts, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	var actual []float64
	err = walkAudio(context.Background(), path, nil, func(frame AudioFrame, _ []byte) error { actual = append(actual, frame.Time); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(timestamps) {
		t.Fatal("PCM frame boundaries lost")
	}
	for i, ts := range actual {
		if math.Abs(ts-timestamps[i]) > .000001 {
			t.Fatalf("timestamp %d changed: %.9f versus %.9f", i, ts, timestamps[i])
		}
	}
}
