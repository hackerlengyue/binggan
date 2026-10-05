package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
)

// Reuse the application's own icon; MyGo scales it for the system status area.
//
//go:embed resources/icon.png
var windowTrayIcon []byte

func showMainWindow(window *mygo.Window) {
	if window == nil || window.IsDestroyed() {
		return
	}
	mygo.App.Show()
	if window.IsMinimized() {
		window.Restore()
	}
	window.Show()
	window.Focus()
}

func windowTrayMenu(window *mygo.Window) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{ID: "show-window", Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { showMainWindow(window) }},
		{ID: "hide-window", Label: "隐藏到菜单栏", Click: func(*mygo.MenuItem, *mygo.Window) { window.Hide() }},
		mygo.Separator(),
		{ID: "quit", Label: "退出饼干大小姐", Role: mygo.RoleQuit},
	})
}

// installWindowTray hides the existing WebView instead of destroying it, keeping
// the page, orchestration state and capture channel alive in the background.
func installWindowTray(window *mygo.Window, lifecycle *LifecycleService) func() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    windowTrayIcon,
		ToolTip: "饼干大小姐 · 关闭窗口后继续在后台运行",
		Menu:    windowTrayMenu(window),
	})
	if err != nil {
		log.Printf("无法创建托盘，将使用 Dock 最小化：%v", err)
	}
	offClose := window.OnClose(func(e *mygo.CloseEvent) {
		if lifecycle.windowMayClose() {
			return
		}
		e.PreventDefault()
		if tray != nil {
			window.Hide()
		} else {
			window.Minimize()
		}
	})
	offActivate := mygo.App.OnActivate(func(visible bool) {
		if !visible && !lifecycle.windowMayClose() {
			showMainWindow(window)
		}
	})
	return func() {
		offClose()
		offActivate()
		if tray != nil {
			tray.Destroy()
		}
	}
}
