//go:build darwin

package main

import (
	"fmt"
	"os"

	"time.haomen/binggan/v2/internal/system"
	"time.haomen/binggan/v2/internal/tunnel"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "用法：capture-service --install|--serve|--uninstall 用户ID")
		os.Exit(2)
	}
	uid, err := system.CaptureServiceUID(os.Args[2])
	if err == nil {
		switch os.Args[1] {
		case "--install":
			var executable string
			executable, err = os.Executable()
			if err == nil {
				err = system.InstallCaptureService(executable, uid)
			}
		case "--serve":
			err = tunnel.RunCaptureService(uid)
		case "--uninstall":
			err = system.UninstallCaptureService(uid)
		default:
			err = fmt.Errorf("未知的操作：%s", os.Args[1])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
