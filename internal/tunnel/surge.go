package tunnel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/surge"
)

func launchSurgeHelper(ctx context.Context, executable, base, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(executable, "--surge-capture-helper", base, token)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// This unprivileged companion outlives a crashed window just long enough to
// remove its temporary Surge rule and policy. It never starts another TUN.
func RunSurgeHelper(base, token string) (result error) {
	if !capture.IsLocalURL(base) || len(token) != 64 {
		return fmt.Errorf("无效的本机控制连接")
	}
	client := capture.LocalClient()
	defer client.CloseIdleConnections()
	d, err := readLease(client, base, token)
	if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			report(client, base, token, Report{Status: "error", Level: "error", Message: result.Error()})
		}
		message := "Surge 深造分流已退出"
		if result != nil {
			message = "Surge 分流助手已退出，请检查上方恢复结果"
		}
		report(client, base, token, Report{Status: "stopped", Message: message})
	}()
	root, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "饼干大小姐 V2", "capture", "surge")
	m, err := surge.New(dir)
	if err != nil {
		return err
	}
	lock := flock.New(filepath.Join(dir, "helper.lock"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	cancel()
	if err != nil || !locked {
		return fmt.Errorf("上次 Surge 分流尚未释放，请稍后重试")
	}
	defer lock.Unlock()
	if err = m.Recover(context.Background()); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := m.Disable(ctx); err != nil {
			result = fmt.Errorf("恢复 Surge 原有分流失败：%w", err)
		}
	}()
	var applied *uint64
	failures := 0
	lastChecked := time.Now()
	for {
		if applied == nil || *applied != d.RoutingRevision {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if d.CaptureEnabled {
				err = m.Enable(ctx, d)
			} else {
				err = m.Disable(ctx)
			}
			cancel()
			if err != nil {
				return err
			}
			revision := d.RoutingRevision
			applied = &revision
			message := "Surge 共存已就绪 · 等待开始提取"
			if d.CaptureEnabled {
				message = "Surge 已仅将深造流量转交抓包，其余分流保持原样"
			}
			report(client, base, token, Report{Status: "ready", Message: message, RoutingRevision: applied})
		}
		if d.CaptureEnabled && time.Since(lastChecked) > 3*time.Second {
			if err = m.Check(context.Background()); err != nil {
				return err
			}
			lastChecked = time.Now()
		}
		time.Sleep(200 * time.Millisecond)
		current, e := readLease(client, base, token)
		if e != nil {
			failures++
			if failures >= 3 {
				return nil
			}
			continue
		}
		failures = 0
		d = current
	}
}
