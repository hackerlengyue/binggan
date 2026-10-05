//go:build darwin

package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"time.haomen/binggan/v2/internal/capture"
	"time.haomen/binggan/v2/internal/system"
)

// RunCaptureService accepts capture requests only from the user who authorized
// installation. The daemon stays alive while each TUN session follows its lease.
func RunCaptureService(uid int) error {
	if os.Geteuid() != 0 || uid <= 0 {
		return fmt.Errorf("流量接管服务必须由管理员启动并指定用户")
	}
	return serveCaptureService(context.Background(), system.CaptureServiceSocket(uid), uid, true, RunHelper, nil)
}

func capturePeerUID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credential *unix.Xucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credential, socketErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if socketErr != nil {
		return 0, socketErr
	}
	return int(credential.Uid), nil
}

func serveCaptureService(ctx context.Context, socketPath string, uid int, changeOwner bool, run func(string, string) error, ready chan<- struct{}) error {
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("流量接管连接路径不是 socket：%s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return err
	}
	if changeOwner {
		if err := os.Chown(socketPath, uid, -1); err != nil {
			return err
		}
	}
	if ready != nil {
		close(ready)
	}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			_ = listener.Close()
		}()
	}
	var activeMu sync.Mutex
	active := false
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			respond := func(response system.CaptureServiceResponse) {
				_ = json.NewEncoder(conn).Encode(response)
			}
			peerUID, err := capturePeerUID(conn)
			if err != nil || peerUID != uid {
				respond(system.CaptureServiceResponse{Error: "当前用户没有流量接管权限"})
				return
			}
			var request system.CaptureServiceRequest
			if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&request); err != nil {
				respond(system.CaptureServiceResponse{Error: "流量接管请求格式无效"})
				return
			}
			if request.Protocol != system.CaptureServiceProtocol {
				respond(system.CaptureServiceResponse{UpgradeRequired: true})
				return
			}
			if !system.ValidCaptureServiceRequest(request) {
				respond(system.CaptureServiceResponse{Error: "流量接管请求无效"})
				return
			}
			activeMu.Lock()
			if active {
				activeMu.Unlock()
				respond(system.CaptureServiceResponse{Error: "已有流量接管任务，请先停止当前任务"})
				return
			}
			active = true
			activeMu.Unlock()
			release := func() {
				activeMu.Lock()
				active = false
				activeMu.Unlock()
			}
			client := capture.LocalClient()
			_, err = readLease(client, request.URL, request.Token)
			client.CloseIdleConnections()
			if err != nil {
				release()
				respond(system.CaptureServiceResponse{Error: "无法读取桌面流量接管配置：" + err.Error()})
				return
			}
			respond(system.CaptureServiceResponse{OK: true})
			go func() {
				defer release()
				if err := run(request.URL, request.Token); err != nil {
					fmt.Fprintln(os.Stderr, "流量接管退出：", err)
				}
			}()
		}()
	}
}
