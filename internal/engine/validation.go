package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
)

// Both original checks still run over the final file. Parallel execution only
// overlaps their work; output is not published until both checks succeed.
func validateMedia(ctx context.Context, path string, before Scan, restored bool, log LogFunc, progress func(float64, float64)) (Scan, []int, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var mu sync.Mutex
	fractions := [2]float64{}
	if !restored {
		fractions[0] = 1
	}
	update := func(index int, done, total float64) {
		if total <= 0 || progress == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fractions[index] = math.Max(fractions[index], math.Min(1, math.Max(0, done/total)))
		progress(fractions[0]+fractions[1], 2)
	}
	if log != nil {
		log("INFO", "正在校验输出文件 · 完整媒体解码与音频检查并行执行")
	}
	decoded := make(chan struct{})
	go func() {
		defer close(decoded)
		err := runMedia(ctx, log, math.Max(before.End-before.First, 1), func(done, total float64) { update(1, done, total) }, "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:v", "-map", "0:a?", "-f", "null", "-")
		if err != nil {
			cancel(fmt.Errorf("完整媒体解码校验失败：%w", err))
		}
	}()
	after := before
	var scanErr error
	if restored {
		after, _, scanErr = ScanAudio(ctx, path, log, func(done, total float64) { update(0, done, total) })
		if scanErr != nil {
			cancel(fmt.Errorf("输出音频检查失败：%w", scanErr))
		} else if math.Abs(after.First-before.First) > .05 || math.Abs(after.End-before.End) > .1 {
			cancel(errors.New("还原后的音频起止时间与输入不一致"))
		}
	}
	energyLoss := make([]int, 0)
	if scanErr == nil {
		rms := map[int]float64{}
		for _, v := range after.Seconds {
			rms[v.Second] = v.RMS
		}
		for _, v := range before.Seconds {
			if v.RMS >= .005 && rms[v.Second] < v.RMS*.1 {
				energyLoss = append(energyLoss, v.Second)
			}
		}
		if after.High > 0 || len(energyLoss) > 0 {
			cancel(fmt.Errorf("音频校验失败：高频异常 %d 秒，能量丢失 %d 秒", after.High, len(energyLoss)))
		}
	}
	// Join even on failure so no decoder keeps reading a deleted temporary file.
	<-decoded
	return after, energyLoss, context.Cause(ctx)
}
