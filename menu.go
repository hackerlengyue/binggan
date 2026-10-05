package main

import (
	"fmt"

	"github.com/egoist/mygo"
)

func (a *Workbench) menu() *mygo.Menu {
	sections := []struct{ title, path string }{
		{"数据概览", "/overview"}, {"秘钥提取", "/capture"},
		{"秘钥管理", "/history"}, {"播放编排", "/player-orchestration"},
		{"视频解密", "/decrypt"}, {"资源管理", "/resources"},
		{"证书管理", "/certificates"}, {"环境信息", "/environment"},
		{"解密设置", "/decrypt-settings"}, {"电源管理", "/power-settings"},
		{"通知管理", "/notifications"},
		{"日志信息", "/logs"},
	}
	view := make([]*mygo.MenuItem, 0, len(sections)+2)
	for i, section := range sections {
		section := section
		item := &mygo.MenuItem{Label: section.title, Click: func(_ *mygo.MenuItem, win *mygo.Window) {
			if win == nil {
				win = a.window
			}
			if win != nil {
				showMainWindow(win)
				_ = Navigate.Emit(win, section.path)
			}
		}}
		if i < 9 {
			item.Accelerator = fmt.Sprintf("CmdOrCtrl+%d", i+1)
		}
		view = append(view, item)
	}
	view = append(view, mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleClose, Label: "隐藏窗口"},
		&mygo.MenuItem{Role: mygo.RoleToggleFullScreen, Label: "进入/退出全屏"},
	)
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu},
		{Label: "查看", Submenu: view}, {Role: mygo.RoleWindowMenu},
	})
}
