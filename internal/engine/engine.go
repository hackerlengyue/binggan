package engine

import (
	"context"
	"crypto/rc4"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func Run(ctx context.Context, input, output string, k Key, appHash string, log LogFunc, progress ProgressFunc) (result map[string]any, err error) {
	started := time.Now()
	if log == nil {
		log = func(string, string) {}
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	stageStart := time.Now()
	last := time.Time{}
	stage := "prepare"
	timings := make(map[string]float64)
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s失败：%w", StageName(stage), err)
		}
	}()
	emit := func(done, total float64) {
		if time.Since(last) < 500*time.Millisecond && done < total {
			return
		}
		last = time.Now()
		progress(Progress{Stage: stage, Done: done, Total: total, Elapsed: time.Since(started).Seconds(), StageElapsed: time.Since(stageStart).Seconds()})
	}
	transition := func(s string) {
		elapsed := time.Since(stageStart).Seconds()
		timings[stage] = elapsed
		log("INFO", fmt.Sprintf("%s完成 · 耗时 %.1f 秒", StageName(stage), elapsed))
		stage = s
		stageStart = time.Now()
		last = time.Time{}
		if s != "complete" {
			log("INFO", "开始"+StageName(s))
		}
		emit(0, 1)
	}
	if err := k.Validate(); err != nil {
		return nil, err
	}
	mima, err := Mima(k.Data, appHash)
	if err != nil {
		return nil, err
	}
	mima = Transform(mima)
	den, _ := number(k.Data["den"])
	pmn, _ := number(k.Data["pmn"])
	src, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return nil, err
	}
	samples, err := Samples(src, info.Size())
	if err != nil {
		return nil, err
	}
	raw, err := os.CreateTemp(filepath.Dir(output), ".decrypted-*.mp4")
	if err != nil {
		return nil, err
	}
	defer os.Remove(raw.Name())
	defer raw.Close()
	log("INFO", fmt.Sprintf("输入文件：%s · 大小 %.2f MiB\n待解密采样单元：%d 个 · 可用密钥段：%d 个", filepath.Base(input), float64(info.Size())/(1<<20), len(samples), len(k.Passwords)))
	if _, err = io.Copy(raw, contextReader{ctx, src}); err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 1<<20)
	transition("decrypt")
	for i, s := range samples {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		idx := segment(s.Offset, den, pmn)
		pwd, ok := k.Passwords[strconv.FormatInt(idx, 10)]
		if !ok {
			return nil, fmt.Errorf("缺少第 %d 段密钥（文件偏移 %d）", idx, s.Offset)
		}
		if cap(buf) < s.Size {
			buf = make([]byte, s.Size)
		}
		buf = buf[:s.Size]
		if _, err = raw.ReadAt(buf, s.Offset); err != nil {
			return nil, err
		}
		c, e := rc4.NewCipher(packetKey(mima, pwd, s.Size))
		if e != nil {
			return nil, e
		}
		c.XORKeyStream(buf, buf)
		if _, err = raw.WriteAt(buf, s.Offset); err != nil {
			return nil, err
		}
		emit(float64(i+1), float64(len(samples)))
	}
	ratio, err := ValidateNAL(raw, samples)
	if err != nil {
		return nil, err
	}
	if ratio < .95 {
		return nil, fmt.Errorf("视频密钥校验失败，NAL 通过率 %.2f%%", ratio*100)
	}
	log("INFO", fmt.Sprintf("视频密钥校验通过 · 视频结构通过率 %.2f%%", ratio*100))
	if err = raw.Sync(); err != nil {
		return nil, err
	}
	if err = raw.Close(); err != nil {
		return nil, err
	}
	transition("scan")
	before, stream, err := scanInputAudio(ctx, raw.Name(), log, emit)
	if err != nil {
		return nil, err
	}
	interval, source, err := ChooseInterval(k, before)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		log("INFO", "未检测到音轨 · 跳过音频修复")
	} else {
		log("INFO", fmt.Sprintf("音频检查结果：%d 帧 · 时长 %.1f 秒 · 高频异常 %d 秒", before.Frames, before.End-before.First, before.High))
		if interval > 0 {
			log("INFO", fmt.Sprintf("修复参数：每 %d 秒切换音频分段 · 参数来源：%s", interval, intervalSourceName(source)))
		} else {
			log("INFO", "无需修复音频 · "+intervalSourceName(source))
		}
	}
	candidate := raw.Name()
	count := 0
	restored := stream != nil && (interval > 0 || len(before.Concealed) > 0)
	if restored {
		transition("restore")
		f, e := os.CreateTemp(filepath.Dir(output), ".restored-*.mp4")
		if e != nil {
			return nil, e
		}
		candidate = f.Name()
		f.Close()
		defer os.Remove(candidate)
		count, err = RestoreAudio(ctx, raw.Name(), candidate, *stream, before, interval, log, emit)
		if err != nil {
			return nil, err
		}
	}
	transition("validate")
	after, energyLoss, err := validateMedia(ctx, candidate, before, restored, log, emit)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(candidate)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	finalSamples, err := Samples(f, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	ratio, err = ValidateNAL(f, finalSamples)
	f.Close()
	if err != nil || ratio < .95 {
		return nil, errors.New("最终视频结构校验失败")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.Rename(candidate, output); err != nil {
		return nil, err
	}
	transition("complete")
	emit(1, 1)
	log("INFO", fmt.Sprintf("校验结果：视频结构通过 · 完整解码通过 · 音频高频异常 %d 秒 · 音频能量丢失 %d 秒", after.High, len(energyLoss)))
	if len(before.Concealed) > 0 {
		log("WARN", fmt.Sprintf("输出包含局部有损修复：%d 个不可解码 AAC 包已替换为静音，并在前后各 5 毫秒平滑过渡，位置见校验报告；不代表原始音频无损恢复", len(before.Concealed)))
	}
	log("INFO", fmt.Sprintf("处理结果：MP4 已生成 · 大小 %.2f MiB · 修复音频 %d 帧 · 总耗时 %.1f 秒", float64(info.Size())/(1<<20), count, time.Since(started).Seconds()))
	result = map[string]any{"engine": "go", "samples": len(samples), "size": info.Size(), "elapsed_sec": time.Since(started).Seconds(), "timings_sec": timings, "audio_restore": map[string]any{"interval_seconds": interval, "parameter_source": source, "restored_frames": count, "before": before, "after": after}, "check": map[string]any{"valid": true, "nal_pass_ratio": ratio, "audio_energy_loss_seconds": energyLoss}}
	b, _ := json.MarshalIndent(result, "", "  ")
	log("RESULT", "完整校验数据（供排查使用）\n"+string(b))
	return result, nil
}
