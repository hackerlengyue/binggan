package engine

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"gonum.org/v1/gonum/dsp/fourier"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// audioDecodeError marks failures eligible for packet-level inspection. FFmpeg
// stderr varies by version and may be empty; it is diagnostic text, not an API.
type audioDecodeError struct{ err error }

func (e *audioDecodeError) Error() string { return e.err.Error() }
func (e *audioDecodeError) Unwrap() error { return e.err }

type AudioStream struct {
	Codec    string `json:"codec_name"`
	Profile  string `json:"profile"`
	Rate     string `json:"sample_rate"`
	Channels int    `json:"channels"`
	Start    string `json:"start_time"`
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
}
type AudioFrame struct {
	Time              float64
	Samples, Channels int
	Format            string
}
type Second struct {
	Second       int     `json:"second"`
	RMS          float64 `json:"rms"`
	HighFraction float64 `json:"high_fraction"`
}
type Scan struct {
	Concealed []AACConcealment `json:"concealed_packets,omitempty"`
	Frames    int              `json:"frames"`
	First     float64          `json:"first_pts"`
	End       float64          `json:"last_end"`
	High      int              `json:"high_frequency_seconds"`
	Gaps      [][2]float64     `json:"gaps"`
	Seconds   []Second         `json:"-"`
}

func probeAudio(ctx context.Context, path string, log LogFunc) (*AudioStream, error) {
	b, err := output(ctx, "ffprobe", log, "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "a", "-show_streams", "-of", "json", path)
	if err != nil {
		return nil, err
	}
	var v struct {
		Streams []AudioStream `json:"streams"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if len(v.Streams) > 1 {
		return nil, errors.New("暂不支持多音轨，不能确认所有音轨均已还原")
	}
	if len(v.Streams) == 0 {
		return nil, nil
	}
	return &v.Streams[0], nil
}

// Walk two bounded pipes in frame order. ffprobe and ffmpeg use the same decoder;
// exact sample counts are checked so a decoder mismatch can never silently shift audio.
func walkAudio(ctx context.Context, path string, log LogFunc, visit func(AudioFrame, []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	probeArgs := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "a:0", "-show_frames", "-show_entries", "frame=best_effort_timestamp_time,nb_samples,sample_fmt,channels", "-of", "compact=p=0"}
	decodeArgs := []string{"-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe"}
	probe, pd := command(ctx, "ffprobe", log, append(probeArgs, path)...)
	decode, dd := command(ctx, "ffmpeg", log, append(decodeArgs, "-i", path, "-map", "0:a:0", "-f", "f32le", "-c:a", "pcm_f32le", "pipe:1")...)
	meta, err := probe.StdoutPipe()
	if err != nil {
		return err
	}
	pcm, err := decode.StdoutPipe()
	if err != nil {
		return err
	}
	if err = probe.Start(); err != nil {
		return err
	}
	if err = decode.Start(); err != nil {
		cancel()
		probe.Wait()
		return err
	}
	var visitErr error
	var sampleReadFailed bool
	scanner := bufio.NewScanner(meta)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	buf := make([]byte, 0, 8192)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "nb_samples=") {
			continue
		}
		fields := map[string]string{}
		for _, part := range strings.Split(line, "|") {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				fields[k] = v
			}
		}
		f := AudioFrame{Format: fields["sample_fmt"]}
		f.Time, err = strconv.ParseFloat(fields["best_effort_timestamp_time"], 64)
		if err != nil || math.IsNaN(f.Time) || math.IsInf(f.Time, 0) {
			visitErr = errors.New("音频缺少有效时间戳")
			break
		}
		f.Samples, _ = strconv.Atoi(fields["nb_samples"])
		f.Channels, _ = strconv.Atoi(fields["channels"])
		if f.Samples < 1 || f.Samples > 65536 || f.Channels < 1 || f.Channels > 8 {
			visitErr = errors.New("音频帧参数无效")
			break
		}
		n := f.Samples * f.Channels * 4
		if cap(buf) < n {
			buf = make([]byte, n)
		}
		buf = buf[:n]
		if _, err = io.ReadFull(pcm, buf); err != nil {
			sampleReadFailed = true
			visitErr = &audioDecodeError{fmt.Errorf("音频解码帧与采样不匹配（预期帧时间 %.3f 秒）：%w", f.Time, err)}
			break
		}
		if err = visit(f, buf); err != nil {
			visitErr = err
			break
		}
	}
	if visitErr == nil {
		visitErr = scanner.Err()
	}
	if visitErr == nil {
		var one [1]byte
		n, e := pcm.Read(one[:])
		if n != 0 || e != io.EOF {
			visitErr = errors.New("音频解码产生多余采样")
		}
	}
	if visitErr != nil {
		cancel()
	}
	pe := probe.Wait()
	de := decode.Wait()
	if visitErr != nil {
		// Wait has drained stderr; retain the decoder cause behind a short PCM read.
		if sampleReadFailed && strings.TrimSpace(dd.tail) != "" {
			return fmt.Errorf("%w；FFmpeg 解码诊断：%s", visitErr, strings.TrimSpace(dd.tail))
		}
		return visitErr
	}
	if pe != nil {
		return fmt.Errorf("音频帧检测失败：%w；%s", pe, pd.tail)
	}
	if de != nil {
		return &audioDecodeError{fmt.Errorf("音频解码失败：%w；%s", de, dd.tail)}
	}
	return nil
}
func ScanAudio(ctx context.Context, path string, log LogFunc, progress func(float64, float64)) (Scan, *AudioStream, error) {
	result := Scan{Gaps: make([][2]float64, 0), Seconds: make([]Second, 0)}
	stream, err := probeAudio(ctx, path, log)
	if err != nil || stream == nil {
		if progress != nil {
			progress(1, 1)
		}
		return result, stream, err
	}
	rate, err := strconv.Atoi(stream.Rate)
	if err != nil || rate < 1 {
		return result, stream, errors.New("音频采样率无效")
	}
	duration, _ := strconv.ParseFloat(stream.Duration, 64)
	type bucket struct {
		energy      float64
		n           int64
		high, total float64
	}
	bins := map[int]*bucket{}
	ffts := map[int]*fourier.FFT{}
	var values []float64
	var coeff []complex128
	err = walkAudio(ctx, path, log, func(f AudioFrame, buf []byte) error {
		sec := int(f.Time)
		b := bins[sec]
		if b == nil {
			b = &bucket{}
			bins[sec] = b
		}
		fft := ffts[f.Samples]
		if fft == nil {
			fft = fourier.NewFFT(f.Samples)
			ffts[f.Samples] = fft
		}
		if len(values) != f.Samples {
			values = make([]float64, f.Samples)
			coeff = make([]complex128, f.Samples/2+1)
		}
		for ch := 0; ch < f.Channels; ch++ {
			for i := 0; i < f.Samples; i++ {
				v := float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[(i*f.Channels+ch)*4:])))
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return errors.New("音频包含无效采样")
				}
				values[i] = v
				b.energy += v * v
				b.n++
			}
			fft.Coefficients(coeff, values)
			for i, v := range coeff {
				p := real(v)*real(v) + imag(v)*imag(v)
				b.total += p
				if float64(i*rate)/float64(f.Samples) > 10000 {
					b.high += p
				}
			}
		}
		if result.Frames == 0 {
			result.First = f.Time
		} else if f.Time-result.End > .1 {
			result.Gaps = append(result.Gaps, [2]float64{result.End, f.Time})
		}
		result.End = f.Time + float64(f.Samples)/float64(rate)
		result.Frames++
		if progress != nil {
			progress(math.Max(0, result.End-result.First), duration)
		}
		return nil
	})
	for sec, b := range bins {
		v := Second{sec, math.Sqrt(b.energy / float64(b.n)), b.high / math.Max(b.total, 1e-30)}
		result.Seconds = append(result.Seconds, v)
		if v.RMS >= .001 && v.HighFraction > .9 {
			result.High++
		}
	}
	sort.Slice(result.Seconds, func(i, j int) bool { return result.Seconds[i].Second < result.Seconds[j].Second })
	if err == nil && progress != nil {
		progress(duration, duration)
	}
	return result, stream, err
}
func ChooseInterval(k Key, scan Scan) (int, string, error) {
	var states []Second
	for _, v := range scan.Seconds {
		if v.RMS >= .001 && (v.HighFraction > .9 || v.HighFraction < .2) {
			states = append(states, v)
		}
	}
	matches := func(interval int) float64 {
		if len(states) == 0 {
			return 1
		}
		n := 0
		for _, v := range states {
			if (v.HighFraction > .9) == (v.Second/interval%2 == 0) {
				n++
			}
		}
		return float64(n) / float64(len(states))
	}
	if raw, ok := k.Data["audio"]; ok {
		v, err := number(raw)
		if err != nil || v < 0 || v > 2147483 {
			return 0, "", errors.New("audio 必须是 0–2147483 的整数秒数")
		}
		if v > 0 && matches(int(v)) < .98 {
			return 0, "", errors.New("audio 参数与实测音频分段不一致，拒绝重编码")
		}
		return int(v), "getPwdData.audio", nil
	}
	if scan.High == 0 {
		return 0, "no_scrambling_detected", nil
	}
	normal := -1
	for _, v := range states {
		if v.HighFraction < .2 {
			normal = v.Second
			break
		}
	}
	if len(states) == 0 || states[0].HighFraction <= .9 || normal <= 0 {
		return 0, "", errors.New("无法可靠推断音频间隔，请补充原始 audio 参数")
	}
	back := -1
	for _, v := range states {
		if v.Second > normal && v.HighFraction > .9 {
			back = v.Second
			break
		}
	}
	if back < 0 || math.Abs(float64(back-2*normal)) > 1 || matches(normal) < .98 {
		return 0, "", errors.New("音频变化不符合固定分段规则，请提供原始 audio 参数")
	}
	return normal, "inferred_from_spectrum", nil
}
func RestoreAudio(ctx context.Context, source, dest string, stream AudioStream, scan Scan, interval int, log LogFunc, progress func(float64, float64)) (int, error) {
	if log == nil {
		log = func(string, string) {}
	}
	if stream.Codec != "aac" || stream.Channels != 2 {
		return 0, errors.New("仅支持双声道 AAC 音频还原")
	}

	rate, err := strconv.Atoi(stream.Rate)
	if err != nil || rate < 1 || interval < 0 {
		return 0, errors.New("音频采样率或修复间隔无效")
	}
	bitrate, _ := strconv.Atoi(stream.BitRate)
	if bitrate < 192000 {
		bitrate = 192000
	}
	end := scan.End
	if d, e := strconv.ParseFloat(stream.Duration, 64); e == nil {
		start, _ := strconv.ParseFloat(stream.Start, 64)
		if start+d < end {
			end = start + d
		}
	}
	encoder, fallback, err := audioEncoder(ctx)
	if err != nil {
		return 0, err
	}
	encode := func(name string) (int, bool, error) {
		args := []string{"-nostdin", "-v", "error", "-xerror", "-y", "-copyts", "-protocol_whitelist", "file,pipe", "-i", source, "-itsoffset", strconv.FormatFloat(scan.First, 'f', 9, 64), "-f", "matroska", "-i", "pipe:0", "-map", "0:v", "-map", "1:a", "-map_metadata", "0", "-c:v", "copy", "-c:a", name, "-b:a", strconv.Itoa(bitrate)}
		if name == "aac_at" {
			// FFmpeg maps 0 to AudioToolbox's highest quality setting here.
			args = append(args, "-aac_at_quality", "0")
		}
		args = append(args, "-af", fmt.Sprintf("atrim=end=%.9f", end), "-movie_timescale", "4410000", "-movflags", "+faststart", dest)
		label := "FFmpeg AAC"
		if name == "aac_at" {
			label = "macOS AudioToolbox AAC"
		}
		log("INFO", fmt.Sprintf("正在修复并编码音频 · 编码器：%s · 目标码率：%d kbps · 视频保持原始编码", label, bitrate/1000))
		return streamRestoredAudio(ctx, source, rate, scan, interval, log, progress, args)
	}
	count, encoderFailed, err := encode(encoder)
	if err != nil && encoderFailed && fallback && ctx.Err() == nil {
		log("WARN", fmt.Sprintf("AudioToolbox 编码失败，切换至兼容编码器重试。原始原因：%s", err))
		// A consumed pipe cannot be replayed. Decode the original audio again so
		// fallback starts at its first frame with the same timestamps.
		count, _, err = encode("aac")
	}
	return count, err
}

// An OS pipe bounds memory and lets FFmpeg consume repaired, timestamped frames
// immediately. Closing the read end releases a producer blocked by a failed or
// cancelled encoder; every exit waits for the producer and its subprocesses.
func streamRestoredAudio(ctx context.Context, source string, rate int, scan Scan, interval int, log LogFunc, progress func(float64, float64), args []string) (int, bool, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	r, w, err := os.Pipe()
	if err != nil {
		return 0, false, err
	}
	defer r.Close()
	type produced struct {
		count int
		err   error
	}
	done := make(chan produced, 1)
	go func() {
		count, err := writeRestoredAudio(ctx, w, source, rate, scan, interval, log)
		// EPIPE means the consumer closed first. Preserve its actual error,
		// particularly when AudioToolbox needs to fall back to native AAC.
		if err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, os.ErrClosed) {
			cancel(fmt.Errorf("音频修复失败：%w", err))
		}
		w.Close()
		done <- produced{count, err}
	}()
	err = runMediaInput(ctx, log, scan.End-scan.First, progress, r, args...)
	encoderFailed := err != nil && ctx.Err() == nil
	if err != nil {
		cancel(err)
	}
	r.Close()
	result := <-done
	if cause := context.Cause(ctx); cause != nil {
		return result.count, encoderFailed, cause
	}
	return result.count, false, result.err
}

func writeRestoredAudio(ctx context.Context, dest io.Writer, source string, rate int, scan Scan, interval int, log LogFunc) (int, error) {
	w := bufio.NewWriterSize(dest, 1<<20)
	if err := pcmHeader(w, rate); err != nil {
		return 0, err
	}
	count := 0
	frameIndex := 0
	expected := scan.First
	err := walkAudio(ctx, source, log, func(frame AudioFrame, b []byte) error {
		first := frameIndex == 0
		frameIndex++
		if frame.Time < expected-2/float64(rate) {
			return errors.New("音频时间戳重叠，无法还原")
		}
		expected = frame.Time + float64(frame.Samples)/float64(rate)
		if interval > 0 && int(frame.Time)/interval%2 == 0 {
			last := math.Abs(expected-scan.End) <= 2/float64(rate)
			if frame.Format != "fltp" || frame.Samples > 1024 || frame.Channels != 2 || (frame.Samples < 1024 && !first && !last) {
				return fmt.Errorf("音频帧不符合双声道 fltp / AAC 帧还原条件：时间 %.6f 秒，采样 %d，声道 %d，格式 %s", frame.Time, frame.Samples, frame.Channels, frame.Format)
			}
			start := 1
			if first && frame.Samples < 1024 {
				if last {
					return errors.New("单个短音频帧无法区分首部裁剪与尾部裁剪，无法确认还原采样位置")
				}
				// MP4 edit lists can discard whole AAC frames and a prefix of
				// the first remaining frame. Whole 1024-sample frames preserve
				// parity; an odd prefix means the first retained sample was odd.
				start = 1 - (1024-frame.Samples)%2
			}
			for i := start; i < frame.Samples; i += 2 {
				for ch := 0; ch < frame.Channels; ch++ {
					off := (i*frame.Channels + ch) * 4
					v := binary.LittleEndian.Uint32(b[off:])
					binary.LittleEndian.PutUint32(b[off:], v^(1<<31))
				}
			}
			count++
		}
		smoothConcealedAudio(b, frame, rate, scan.Concealed)
		return pcmFrame(w, frame.Time-scan.First, b)
	})
	if err != nil {
		return count, err
	}
	return count, w.Flush()
}
