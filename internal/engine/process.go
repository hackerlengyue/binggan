package engine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time.haomen/binggan/v2/internal/procutil"
	"time"
)

type LogFunc func(level, message string)
type Progress struct {
	Stage        string  `json:"stage"`
	Done         float64 `json:"done"`
	Total        float64 `json:"total"`
	Elapsed      float64 `json:"elapsed"`
	StageElapsed float64 `json:"stage_elapsed"`
	Unit         string  `json:"unit,omitempty"`
}
type ProgressFunc func(Progress)
type diagnostics struct {
	mu   sync.Mutex
	tail string
	log  LogFunc
}

func (d *diagnostics) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := strings.TrimSpace(string(b))
	if s != "" && d.log != nil {
		d.log("STDERR", s)
	}
	d.tail += string(b)
	if len(d.tail) > 4096 {
		d.tail = d.tail[len(d.tail)-4096:]
	}
	return len(b), nil
}
func command(ctx context.Context, name string, log LogFunc, args ...string) (*exec.Cmd, *diagnostics) {
	c := exec.CommandContext(ctx, name, args...)
	procutil.HideWindow(c)
	c.WaitDelay = 5 * time.Second
	d := &diagnostics{log: log}
	c.Stderr = d
	return c, d
}
func run(ctx context.Context, name string, log LogFunc, args ...string) error {
	c, d := command(ctx, name, log, args...)
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s 执行失败：%w；%s", name, err, strings.TrimSpace(d.tail))
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("工具输出超过限制")
	}
	return b.Buffer.Write(p)
}
func output(ctx context.Context, name string, log LogFunc, args ...string) ([]byte, error) {
	c, d := command(ctx, name, log, args...)
	var b = limitedBuffer{max: 1 << 20}
	c.Stdout = &b
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("%s 失败：%w；%s", name, err, d.tail)
	}
	return b.Bytes(), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// FFmpeg emits machine-readable progress independently of its diagnostic log.
func runMedia(ctx context.Context, log LogFunc, total float64, progress func(float64, float64), args ...string) error {
	return runMediaInput(ctx, log, total, progress, nil, args...)
}

func runMediaInput(ctx context.Context, log LogFunc, total float64, progress func(float64, float64), input io.Reader, args ...string) error {
	args = append([]string{"-progress", "pipe:1", "-stats_period", "0.5", "-nostats"}, args...)
	c, d := command(ctx, "ffmpeg", log, args...)
	c.Stdin = input
	pipe, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	if err = c.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && key == "out_time_us" && progress != nil {
			if n, e := strconv.ParseFloat(value, 64); e == nil {
				progress(math.Min(total, math.Max(0, n/1e6)), total)
			}
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		c.Process.Kill()
	}
	err = c.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanErr != nil {
		return scanErr
	}
	if err != nil {
		return fmt.Errorf("ffmpeg 执行失败：%w；%s", err, strings.TrimSpace(d.tail))
	}
	if progress != nil {
		progress(total, total)
	}
	return nil
}
func StageName(stage string) string {
	if label, ok := map[string]string{"prepare": "输入检查", "decrypt": "视频解密", "scan": "音频检查", "restore": "音频修复", "validate": "最终校验", "complete": "处理完成"}[stage]; ok {
		return label
	}
	return stage
}

func intervalSourceName(source string) string {
	switch source {
	case "getPwdData.audio":
		return "密钥记录中的 audio 参数"
	case "no_scrambling_detected":
		return "未检测到音频扰乱"
	case "inferred_from_spectrum":
		return "根据音频频谱自动识别"
	default:
		return source
	}
}
