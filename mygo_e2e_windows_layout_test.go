//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// Exercise the Windows CSS contract in a real WebView. The native caption
// itself still requires a Windows host; its three buttons occupy 138 DIPs.
func TestMyGoWindowsHeaderLayout(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("set BINGGAN_MYGO_E2E=1 to run the native WebView test")
	}
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("workspace startup timed out")
	}
	w := mygo.NewWindow(mainWindowOptions())
	defer w.Destroy()
	waitMyGoPage(t, w, `document.querySelector('.window-toolbar')`)
	_, err := mygo.EvalAs[bool](w.Page(), `(() => {
		// MyGo's JavaScript platform is win32, including 64-bit Windows.
		document.documentElement.dataset.desktopPlatform = 'win32';
		document.documentElement.style.setProperty('--mygo-titlebar-inset-right', '138px');
		location.hash = '#/environment';
		return true;
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="环境信息"] tbody')?.innerText.includes('Go 运行环境')`)
	for _, size := range []struct{ width, height int }{{1320, 880}, {1000, 880}, {1320, 680}, {1000, 680}} {
		w.SetSize(size.width, size.height)
		waitMyGoPage(t, w, fmt.Sprintf(`innerWidth === %d`, size.width))
		for _, inset := range []int{138, 92, 0} {
			if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => { document.documentElement.style.setProperty('--mygo-titlebar-inset-right', '%dpx'); return true })()`, inset)); err != nil {
				t.Fatal(err)
			}
			geometry, err := mygo.EvalAs[struct {
				Width   float64 `json:"width"`
				Right   float64 `json:"right"`
				Padding float64 `json:"padding"`
				Scroll  float64 `json:"scroll"`
			}](w.Page(), `({width:innerWidth,right:document.querySelector('#workspace-page-actions').getBoundingClientRect().right,padding:parseFloat(getComputedStyle(document.querySelector('.window-toolbar')).paddingRight),scroll:document.documentElement.scrollWidth})`)
			if err != nil || geometry.Right > geometry.Width-float64(inset)-15 || geometry.Scroll > geometry.Width+1 {
				t.Errorf("size=%dx%d inset=%d: actions overlap caption or page overflows: %+v, %v", size.width, size.height, inset, geometry, err)
			}
			t.Logf("size=%dx%d inset=%d geometry=%+v", size.width, size.height, inset, geometry)
		}
	}
	// Other pages teleport actions into this same header; capture uses its slot.
	for _, route := range []struct{ path, label string }{
		{"/decrypt-settings", "解密设置"}, {"/certificates", "证书管理"},
		{"/notifications", "通知管理"}, {"/decrypt", "视频解密"},
		{"/logs", "日志查询"}, {"/capture", "本次秘钥提取"},
	} {
		if _, err := mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
			document.documentElement.style.setProperty('--mygo-titlebar-inset-right','138px');
			location.hash = %q; return true;
		})()`, "#"+route.path)); err != nil {
			t.Fatal(err)
		}
		waitMyGoPage(t, w, fmt.Sprintf(`location.hash === %q && document.querySelector('section[aria-label=%q]')`, "#"+route.path, route.label))
		clear, err := mygo.EvalAs[bool](w.Page(), `(() => {
			const buttons = [...document.querySelectorAll('.window-toolbar button')];
			return buttons.length > 0 && buttons.every(button => button.getBoundingClientRect().right <= innerWidth - 138 - 15) && document.documentElement.scrollWidth <= innerWidth;
		})()`)
		if err != nil || !clear {
			t.Errorf("%s actions overlap caption at minimum window size: %v, %v", route.path, clear, err)
		}
	}
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		w.SetSize(1320, 880)
		_, _ = mygo.EvalAs[bool](w.Page(), `(() => { document.documentElement.style.setProperty('--mygo-titlebar-inset-right','138px'); location.hash='#/decrypt'; return true })()`)
		waitMyGoPage(t, w, `innerWidth === 1320 && document.querySelector('section[aria-label="视频解密"]') && document.querySelector('#workspace-page-actions button')?.textContent.includes('新增解密')`)
		captureMyGoThemes(t, w, "windows-caption-decrypt")
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Clean(out), png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
