package engine

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Concealment is explicitly lossy. Offsets refer to the decrypted input copy;
// packet sizes and the MP4 sample tables remain unchanged.
type AACConcealment struct {
	Time     float64 `json:"time_seconds"`
	Duration float64 `json:"duration_seconds"`
	Offset   int64   `json:"input_offset"`
	Size     int     `json:"packet_bytes"`
	Method   string  `json:"method"`
}

type audioPacket struct {
	pos           int64
	size          int
	pts, duration float64
}

// Apply a raised-cosine envelope after sign restoration. It reaches zero with
// zero slope at each damaged interval and leaves timestamps/sample counts intact.
// This suppresses abrupt mute edges; it cannot reconstruct missing speech.
const concealFadeSeconds = 0.005
const maxConcealedGapSeconds = 0.200
const maxConcealedTotalSeconds = 0.250

func smoothConcealedAudio(pcm []byte, frame AudioFrame, rate int, concealed []AACConcealment) {
	end := frame.Time + float64(frame.Samples)/float64(rate)
	for _, gap := range concealed {
		left, right := gap.Time, gap.Time+gap.Duration
		if end <= left-concealFadeSeconds || frame.Time >= right+concealFadeSeconds {
			continue
		}
		for i := 0; i < frame.Samples; i++ {
			t := frame.Time + float64(i)/float64(rate)
			if t <= left-concealFadeSeconds || t >= right+concealFadeSeconds {
				continue
			}
			gain := 0.0
			if t < left {
				gain = 0.5 - 0.5*math.Cos(math.Pi*(left-t)/concealFadeSeconds)
			} else if t > right {
				gain = 0.5 - 0.5*math.Cos(math.Pi*(t-right)/concealFadeSeconds)
			}
			for ch := 0; ch < frame.Channels; ch++ {
				off := (i*frame.Channels + ch) * 4
				value := math.Float32frombits(binary.LittleEndian.Uint32(pcm[off:]))
				binary.LittleEndian.PutUint32(pcm[off:], math.Float32bits(value*float32(gain)))
			}
		}
	}
}

func scanInputAudio(ctx context.Context, path string, log LogFunc, progress func(float64, float64)) (Scan, *AudioStream, error) {
	scan, stream, err := ScanAudio(ctx, path, log, progress)
	var decodeErr *audioDecodeError
	if err == nil || ctx.Err() != nil || stream == nil || stream.Codec != "aac" || !errors.As(err, &decodeErr) {
		return scan, stream, err
	}
	repaired, recoveryErr := concealAACPackets(ctx, path, *stream, log)
	if recoveryErr != nil {
		return scan, stream, fmt.Errorf("%w；局部容错未执行：%v", err, recoveryErr)
	}
	retry, retryStream, retryErr := ScanAudio(ctx, path, log, progress)
	retry.Concealed = repaired
	if retryErr != nil {
		return retry, retryStream, fmt.Errorf("局部容错后严格音频检查仍失败：%w", retryErr)
	}
	return retry, retryStream, nil
}

// Pair successful decoded frames with their original packets, using positions
// rather than frame numbers (priming/edit lists can discard initial packets).
func missingAACPackets(ctx context.Context, path string, stream AudioStream) ([]audioPacket, error) {
	rate, err := strconv.Atoi(stream.Rate)
	if err != nil || rate <= 0 {
		return nil, errors.New("无效采样率")
	}
	start, err := strconv.ParseFloat(stream.Start, 64)
	if err != nil || math.IsNaN(start) || math.IsInf(start, 0) {
		return nil, errors.New("音频缺少有效起始时间")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, diag := command(ctx, "ffprobe", nil, "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "a:0", "-show_packets", "-show_frames", "-show_entries", "packet=pos,pts_time,duration_time,size:frame=pkt_pos", "-of", "compact=p=1", path)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	var packets []audioPacket
	decoded := map[int64]bool{}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var parseErr error
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) == 0 {
			continue
		}
		fields := map[string]string{}
		for _, part := range parts[1:] {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				fields[k] = v
			}
		}
		if parts[0] == "frame" {
			pos, e := strconv.ParseInt(fields["pkt_pos"], 10, 64)
			if e != nil || pos < 0 {
				parseErr = errors.New("解码帧缺少包位置")
				break
			}
			decoded[pos] = true
			continue
		}
		if parts[0] != "packet" {
			continue
		}
		pos, e1 := strconv.ParseInt(fields["pos"], 10, 64)
		size, e2 := strconv.Atoi(fields["size"])
		pts, e3 := strconv.ParseFloat(fields["pts_time"], 64)
		duration, e4 := strconv.ParseFloat(fields["duration_time"], 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pos < 0 || size < 1 || !finite(pts) || !finite(duration) || duration <= 0 {
			parseErr = errors.New("音频包参数无效，不能安全容错")
			break
		}
		packets = append(packets, audioPacket{pos, size, pts, duration})
		if len(packets) > 5000000 {
			parseErr = errors.New("音频包数量超过限制")
			break
		}
	}
	if parseErr == nil {
		parseErr = scanner.Err()
	}
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, parseErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("音频包定位失败：%w；%s", waitErr, diag.tail)
	}
	var missing []audioPacket
	var total float64
	tolerance := 2 / float64(rate)
	for i := 0; i < len(packets); {
		p := packets[i]
		if decoded[p.pos] || p.pts < start-tolerance {
			i++
			continue
		}
		first := i
		gapDuration := 0.0
		for i < len(packets) && !decoded[packets[i].pos] {
			p = packets[i]
			if math.Abs(p.duration-1024/float64(rate)) > tolerance ||
				(i > first && math.Abs(packets[i-1].pts+packets[i-1].duration-p.pts) > tolerance) {
				return nil, errors.New("坏包帧长或时间轴异常，拒绝自动填补")
			}
			gapDuration += p.duration
			missing = append(missing, p)
			i++
		}
		// Both decoded neighbours must anchor an interior gap. Priming/edit-list
		// packets outside the stream are not damage and cannot serve as anchors.
		if first == 0 || i == len(packets) || !decoded[packets[first-1].pos] ||
			math.Abs(packets[first-1].pts+packets[first-1].duration-packets[first].pts) > tolerance ||
			math.Abs(packets[i-1].pts+packets[i-1].duration-packets[i].pts) > tolerance {
			return nil, errors.New("坏包位于音频边缘或时间轴异常，拒绝自动填补")
		}
		if gapDuration > maxConcealedGapSeconds+tolerance {
			return nil, fmt.Errorf("连续坏包 %.3f 毫秒，超过 200 毫秒，拒绝自动填补", gapDuration*1000)
		}
		total += gapDuration
		if total > maxConcealedTotalSeconds+tolerance {
			return nil, errors.New("坏包累计超过 250 毫秒，拒绝自动填补")
		}
	}
	if len(missing) == 0 {
		return nil, errors.New("未能定位不可解码音频包")
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].pos < missing[j].pos })
	return missing, nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func concealAACPackets(ctx context.Context, path string, stream AudioStream, log LogFunc) ([]AACConcealment, error) {
	if stream.Codec != "aac" || stream.Profile != "LC" || stream.Channels != 2 {
		return nil, errors.New("局部容错仅支持双声道 AAC-LC")
	}
	packets, err := missingAACPackets(ctx, path, stream)
	if err != nil {
		return nil, err
	}
	silent, err := silentAACPacket(ctx, stream.Rate)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	samples, err := Samples(f, info.Size())
	if err != nil {
		return nil, err
	}
	// Validate every target against the container before writing any packet.
	replacements := make([][]byte, len(packets))
	for i, p := range packets {
		index := sort.Search(len(samples), func(i int) bool { return samples[i].Offset >= p.pos })
		if index == len(samples) || samples[index].Video || samples[index].Offset != p.pos || samples[index].Size != p.size {
			return nil, errors.New("音频包位置与 MP4 采样表不一致")
		}
		replacements[i], err = padAACSilence(silent, p.size)
		if err != nil {
			return nil, err
		}
	}
	result := make([]AACConcealment, 0, len(packets))
	for i, p := range packets {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if _, err = f.WriteAt(replacements[i], p.pos); err != nil {
			return nil, err
		}
		result = append(result, AACConcealment{p.pts, p.duration, p.pos, p.size, "silence_replacement_lossy"})
		if log != nil {
			log("WARN", fmt.Sprintf("局部有损修复：%.6f 秒处不可解码 AAC 包已替换为静音（%.3f 毫秒）；包长度与时间轴不变，原始文件未修改", p.pts, p.duration*1000))
		}
	}
	return result, f.Sync()
}

func silentAACPacket(ctx context.Context, rate string) ([]byte, error) {
	n, err := strconv.Atoi(rate)
	if err != nil || n < 7350 || n > 96000 {
		return nil, errors.New("不支持的 AAC 采样率")
	}
	data, err := output(ctx, "ffmpeg", nil, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "anullsrc=r="+strconv.Itoa(n)+":cl=stereo", "-t", "0.5", "-c:a", "aac", "-profile:a", "aac_low", "-f", "adts", "pipe:1")
	if err != nil {
		return nil, err
	}
	for frame := 0; len(data) >= 7; frame++ {
		if data[0] != 0xff || data[1]&0xf6 != 0xf0 {
			return nil, errors.New("静音 AAC ADTS 头无效")
		}
		header := 7
		if data[1]&1 == 0 {
			header = 9
		}
		size := int(data[3]&3)<<11 | int(data[4])<<3 | int(data[5]>>5)
		if size <= header || size > len(data) || data[6]&3 != 0 {
			return nil, errors.New("静音 AAC 帧长度无效")
		}
		if frame == 1 {
			return append([]byte{}, data[header:size]...), nil
		}
		data = data[size:]
	}
	return nil, errors.New("未生成可用静音 AAC 帧")
}

// AAC fill elements precede the untouched silent raw_data_block. Groups of
// eight seven-bit headers (plus optional byte-sized count escapes) align to a
// byte, so the original END element and its padding stay valid. Keeping the
// exact packet size avoids rewriting offsets, sample durations or video bytes.
func padAACSilence(silent []byte, size int) ([]byte, error) {
	if size == len(silent) {
		return append([]byte{}, silent...), nil
	}
	if size > 8191 || size < len(silent)+7 {
		return nil, errors.New("坏包长度不适合安全静音替换")
	}
	delta := size - len(silent)
	for count := 8; count <= 64; count += 8 {
		for escaped := 0; escaped <= count; escaped++ {
			payload := delta - count*7/8 - escaped
			if payload < escaped*15 || payload > escaped*269+(count-escaped)*14 {
				continue
			}
			counts := make([]int, count)
			remaining := payload
			for i := range counts {
				if i < escaped {
					counts[i] = 15
					remaining -= 15
				}
			}
			for i := range counts {
				limit := 14
				if i < escaped {
					limit = 269
				}
				extra := min(remaining, limit-counts[i])
				counts[i] += extra
				remaining -= extra
			}
			out := make([]byte, delta)
			bit := 0
			put := func(v, n int) {
				for j := n - 1; j >= 0; j-- {
					out[bit/8] |= byte((v>>j)&1) << uint(7-bit%8)
					bit++
				}
			}
			for _, n := range counts {
				put(6, 3)
				if n >= 15 {
					put(15, 4)
					put(n-14, 8)
				} else {
					put(n, 4)
				}
				for j := 0; j < n; j++ {
					put(0, 8)
				}
			}
			if bit != delta*8 {
				return nil, errors.New("静音填充长度不一致")
			}
			return append(out, silent...), nil
		}
	}
	return nil, errors.New("无法生成同长度静音 AAC 包")
}
