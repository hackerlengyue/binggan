package playerautomation

import (
	"context"
	"fmt"
	"time"
)

// Wait owns no player state and performs no input, including after cancellation.
func Wait(ctx context.Context, milliseconds int) error {
	if milliseconds < 0 || milliseconds > 30000 {
		return fmt.Errorf("播放编排等待时间须为 0–30000 毫秒")
	}
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
