//go:build windows

package power

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

func startScreenSaverGuard() (func() error, func(), error) {
	user := windows.NewLazySystemDLL("user32.dll")
	spi := user.NewProc("SystemParametersInfoW")
	var blocked uint32
	// Respect the user's policy for synthetic input resetting the saver timer.
	if ok, _, err := spi.Call(0x1026, 0, uintptr(unsafe.Pointer(&blocked)), 0); ok == 0 {
		return nil, nil, fmt.Errorf("无法读取屏保策略：%w", err)
	}
	if blocked != 0 {
		return nil, nil, fmt.Errorf("系统策略禁止应用阻止屏保")
	}
	send := user.NewProc("SendInput")
	openDesktop := user.NewProc("OpenInputDesktop")
	closeDesktop := user.NewProc("CloseDesktop")
	getInfo := user.NewProc("GetUserObjectInformationW")
	// INPUT contains a pointer-aligned MOUSEINPUT union. No movement, buttons,
	// or keyboard events are injected; only the idle timer is refreshed.
	type mouseInput struct {
		X, Y              int32
		Data, Flags, Time uint32
		Extra             uintptr
	}
	type input struct {
		Type  uint32
		Mouse mouseInput
	}
	pulse := func() error {
		desktop, _, _ := openDesktop.Call(0, 0, 0x0100) // DESKTOP_SWITCHDESKTOP
		if desktop == 0 {
			return nil
		} // locked/secure desktop
		defer closeDesktop.Call(desktop)
		var name [256]uint16
		var needed uint32
		ok, _, _ := getInfo.Call(desktop, 2, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed)))
		if ok == 0 || windows.UTF16ToString(name[:]) != "Default" {
			return nil
		}
		event := input{Mouse: mouseInput{Flags: 0x0001}}
		if n, _, err := send.Call(1, uintptr(unsafe.Pointer(&event)), unsafe.Sizeof(event)); n != 1 {
			return fmt.Errorf("无法阻止屏保：%w", err)
		}
		return nil
	}
	return pulse, func() {}, nil
}
