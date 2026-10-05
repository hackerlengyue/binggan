package main

import (
	"errors"

	"github.com/egoist/mygo"
)

// NotificationService sends desktop notifications through MyGo's native API.
type NotificationService struct{ app *Workbench }

func (s *NotificationService) Show(title, body, route string) error {
	switch route {
	case "", "/capture", "/player-orchestration", "/decrypt":
	default:
		return errors.New("通知目标无效")
	}
	if !mygo.NotificationsSupported() {
		return errors.New("当前系统不支持桌面通知")
	}
	notification := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body})
	notification.OnClick(func() {
		if s.app.window != nil {
			showMainWindow(s.app.window)
			if route != "" {
				_ = Navigate.Emit(s.app.window, route)
			}
		}
	})
	return notification.Show()
}
