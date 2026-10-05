package main

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/power"
	"time.haomen/binggan/v2/internal/tasks"
	"time.haomen/binggan/v2/internal/workspace"
)

// WorkspaceService is the typed MyGo boundary for ordinary workspace data.
// Media bytes and downloads use Protocol; capture events use Channel.
type WorkspaceService struct{ workbench *Workbench }

// CopyText uses the native clipboard without auditing potentially sensitive text.
func (s *WorkspaceService) CopyText(ctx context.Context, text string) error {
	if _, err := s.app(ctx); err != nil {
		return err
	}
	mygo.Clipboard.WriteText(text)
	return nil
}

// BootstrapStatus remains callable while workspace services start.
func (s *WorkspaceService) BootstrapStatus() BootstrapStatus {
	if s.workbench == nil {
		return BootstrapStatus{Phase: "启动失败", Error: "工作台尚未启动"}
	}
	return s.workbench.bootstrapStatus()
}

func auditedCall[T any](a *workspace.App, action string, recordSuccess bool, call func() (T, error)) (T, error) {
	start := time.Now()
	value, err := call()
	a.AuditOperation(action, time.Since(start), recordSuccess, err)
	return value, err
}

func (s *WorkspaceService) app(ctx context.Context) (*workspace.App, error) {
	if s.workbench == nil {
		return nil, errors.New("工作台尚未启动")
	}
	if err := s.workbench.wait(ctx); err != nil {
		return nil, err
	}
	if s.workbench.startErr != nil {
		return nil, s.workbench.startErr
	}
	if s.workbench.app == nil {
		return nil, errors.New("工作台尚未就绪")
	}
	return s.workbench.app, nil
}

// plain waits for the workspace and runs call without operation auditing.
func plain[T any](ctx context.Context, s *WorkspaceService, call func(*workspace.App) (T, error)) (T, error) {
	a, err := s.app(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return call(a)
}

// audited waits for the workspace and records the call in the operation log.
func audited[T any](ctx context.Context, s *WorkspaceService, action string, recordSuccess bool, call func(*workspace.App) (T, error)) (T, error) {
	return plain(ctx, s, func(a *workspace.App) (T, error) {
		return auditedCall(a, action, recordSuccess, func() (T, error) { return call(a) })
	})
}

func (s *WorkspaceService) Health(ctx context.Context) (workspace.HealthStatus, error) {
	return audited(ctx, s, "检查服务连接", false, func(a *workspace.App) (workspace.HealthStatus, error) { return a.Health() })
}

// Subscribe streams the initial receiver state followed by capture changes.
// Closing the frontend channel cancels the Go context and releases its hub slot.
func (s *WorkspaceService) Subscribe(ctx context.Context, notices *mygo.Channel[workspace.CaptureNotice]) error {
	_, err := plain(ctx, s, func(a *workspace.App) (struct{}, error) { return struct{}{}, a.Subscribe(ctx, notices.Send) })
	return err
}

func (s *WorkspaceService) History(ctx context.Context, limit int) ([]workspace.Capture, error) {
	return audited(ctx, s, "读取接收记录", false, func(a *workspace.App) ([]workspace.Capture, error) { return a.History(limit) })
}

func (s *WorkspaceService) CaptureState(ctx context.Context) (bool, error) {
	return plain(ctx, s, func(a *workspace.App) (bool, error) { return a.CaptureState(), nil })
}

func (s *WorkspaceService) SetCaptureState(ctx context.Context, enabled bool) (bool, error) {
	return audited(ctx, s, "更新提取状态", true, func(a *workspace.App) (bool, error) {
		// Stop accepting records before dismantling forwarding. On start, wait
		// for forwarding before allowing the player to make its first request.
		if !enabled {
			a.SetCaptureState(false)
		}
		if s.workbench.monitor != nil {
			if err := s.workbench.monitor.setCaptureRouting(ctx, enabled); err != nil {
				return a.CaptureState(), err
			}
		}
		return a.SetCaptureState(enabled), nil
	})
}

func (s *WorkspaceService) ClearTraces(ctx context.Context, clientID string) error {
	_, err := audited(ctx, s, "清空接收记录", true, func(a *workspace.App) (struct{}, error) { return struct{}{}, a.ClearTraces(clientID) })
	return err
}

func (s *WorkspaceService) KeyHistory(ctx context.Context) (string, error) {
	return audited(ctx, s, "读取密钥历史", false, func(a *workspace.App) (string, error) { return a.KeyHistory() })
}

func (s *WorkspaceService) SaveKeyHistory(ctx context.Context, raw string) (string, error) {
	return audited(ctx, s, "保存密钥历史", true, func(a *workspace.App) (string, error) { return a.SaveKeyHistory(raw) })
}

func (s *WorkspaceService) Logs(ctx context.Context, query workspace.LogQuery) (workspace.LogPage, error) {
	return plain(ctx, s, func(a *workspace.App) (workspace.LogPage, error) { return a.Logs(query) })
}

func (s *WorkspaceService) AppendLogs(ctx context.Context, entries []workspace.ClientLog) error {
	_, err := plain(ctx, s, func(a *workspace.App) (struct{}, error) { return struct{}{}, a.AppendLogs(entries) })
	return err
}

func (s *WorkspaceService) ClearLogs(ctx context.Context, scope string) (int64, error) {
	return plain(ctx, s, func(a *workspace.App) (int64, error) { return a.ClearLogs(scope) })
}

func (s *WorkspaceService) JobLogs(ctx context.Context, id string, offset int64) (workspace.JobLogChunk, error) {
	return audited(ctx, s, "读取子任务日志", false, func(a *workspace.App) (workspace.JobLogChunk, error) { return a.JobLogs(id, offset) })
}

func (s *WorkspaceService) Settings(ctx context.Context) (workspace.PlayerSettings, error) {
	return audited(ctx, s, "读取解密配置", false, func(a *workspace.App) (workspace.PlayerSettings, error) { return a.Settings() })
}

func (s *WorkspaceService) SaveSettings(ctx context.Context, input workspace.PlayerSettings) (workspace.PlayerSettings, error) {
	return audited(ctx, s, "保存解密配置", true, func(a *workspace.App) (workspace.PlayerSettings, error) { return a.SaveSettings(input) })
}

func (s *WorkspaceService) Connection(ctx context.Context) (workspace.ConnectionStatus, error) {
	return audited(ctx, s, "读取连接状态", false, func(a *workspace.App) (workspace.ConnectionStatus, error) { return a.Connection() })
}

func (s *WorkspaceService) Diagnose(ctx context.Context) (workspace.Diagnosis, error) {
	return audited(ctx, s, "执行环境检测", true, func(a *workspace.App) (workspace.Diagnosis, error) { return a.Diagnose(ctx) })
}

func (s *WorkspaceService) Resources(ctx context.Context, query workspace.ResourceQuery) (workspace.ResourcePage, error) {
	return audited(ctx, s, "读取视频资源", false, func(a *workspace.App) (workspace.ResourcePage, error) { return a.Resources(query) })
}

func (s *WorkspaceService) Tasks(ctx context.Context) (workspace.TaskList, error) {
	return audited(ctx, s, "读取解密任务", false, func(a *workspace.App) (workspace.TaskList, error) { return a.ListTasks() })
}

func (s *WorkspaceService) Summary(ctx context.Context) (workspace.TaskSummary, error) {
	return plain(ctx, s, func(a *workspace.App) (workspace.TaskSummary, error) { return a.TaskSummary(), nil })
}

func (s *WorkspaceService) CreateTask(ctx context.Context, input workspace.CreateTaskInput) (tasks.View, error) {
	return audited(ctx, s, "创建解密任务", true, func(a *workspace.App) (tasks.View, error) { return a.CreateTask(input) })
}

func (s *WorkspaceService) DeleteTasks(ctx context.Context, ids []string) (tasks.DeleteResult, error) {
	return audited(ctx, s, "删除解密任务", true, func(a *workspace.App) (tasks.DeleteResult, error) { return a.DeleteTasks(ids) })
}

func (s *WorkspaceService) RetryTask(ctx context.Context, taskID string, jobIDs []string) (tasks.View, error) {
	return audited(ctx, s, "重试解密任务", true, func(a *workspace.App) (tasks.View, error) { return a.RetryTask(taskID, jobIDs) })
}

// ChooseVideoFiles opens the native picker attached to the calling window.
func (s *WorkspaceService) ChooseVideoFiles(ctx context.Context) ([]workspace.LocalVideo, error) {
	return audited(ctx, s, "选择视频", true, func(a *workspace.App) ([]workspace.LocalVideo, error) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: mygo.CallerWindow(ctx), Title: "选择 .sz 视频",
			Filters: []mygo.FileFilter{{Name: "SZ 视频", Extensions: []string{"sz"}}}, Multiple: true,
		})
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return a.InspectVideoFiles(paths)
	})
}

// InspectVideoFiles validates paths from MyGo's native file-drop event.
func (s *WorkspaceService) InspectVideoFiles(ctx context.Context, paths []string) ([]workspace.LocalVideo, error) {
	return audited(ctx, s, "检查所选视频", false, func(a *workspace.App) ([]workspace.LocalVideo, error) { return a.InspectVideoFiles(paths) })
}

// ImportVideo streams local copy progress over a channel. Closing it cancels
// the copy and removes the partial file.
func (s *WorkspaceService) ImportVideo(ctx context.Context, choice workspace.LocalVideo, updates *mygo.Channel[workspace.VideoImportProgress]) (workspace.ImportedVideo, error) {
	return audited(ctx, s, "导入视频", true, func(a *workspace.App) (workspace.ImportedVideo, error) {
		return a.ImportVideo(ctx, choice, updates.Send)
	})
}

func (s *WorkspaceService) PowerSettings(ctx context.Context) (result power.Settings, err error) {
	if _, err = s.app(ctx); err != nil {
		return
	}
	mygo.RunOnMain(func() {
		if s.workbench.power == nil {
			err = errors.New("电源设置尚未就绪")
			return
		}
		result, err = s.workbench.power.Settings()
	})
	return
}
func (s *WorkspaceService) SavePowerSettings(ctx context.Context, settings power.Settings) (result power.Settings, err error) {
	if _, err = s.app(ctx); err != nil {
		return
	}
	mygo.RunOnMain(func() {
		if s.workbench.power == nil {
			err = errors.New("电源设置尚未就绪")
			return
		}
		result, err = s.workbench.power.Save(settings)
	})
	return
}
