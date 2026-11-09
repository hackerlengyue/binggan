//go:build darwin

package main

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestMyGoQueueRoutingChannelLifetime(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("native WebView opt-in")
	}
	mygoE2EStart.Do(func() { go mygoE2E.start() })
	select {
	case <-mygoE2E.ready:
		if mygoE2E.startErr != nil {
			t.Fatal(mygoE2E.startErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("startup timed out")
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Queue routing channel test", URL: "/", Width: 900, Height: 680})
	defer w.Destroy()
	waitMyGoPage(t, w, `document.readyState === "complete" && typeof window.mygo !== "undefined"`)
	start := func() {
		t.Helper()
		ok, err := mygo.EvalAs[bool](w.Page(), `(async () => {
			window.queueRouteChannel=window.mygo.channel();
			const ready=new Promise(resolve => { window.queueRouteChannel.onmessage=owner => resolve(typeof owner==='string' && owner.length>0); });
			window.queueRouteDone=window.mygo.call('CaptureService.RoutingSession',window.queueRouteChannel);
			return await ready;
		})()`)
		if err != nil || !ok {
			t.Fatalf("native channel readiness: %t %v", ok, err)
		}
	}
	waitReleased := func() {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			mygoE2E.monitor.op.Lock()
			held := mygoE2E.monitor.routingHeld
			mygoE2E.monitor.op.Unlock()
			if held == "" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("native channel cancellation did not release routing hold")
	}
	start()
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => { window.queueRouteChannel.close(); await window.queueRouteDone; return true; })()`); err != nil {
		t.Fatalf("explicit native cleanup did not acknowledge: %v", err)
	}
	waitReleased()
	start()
	w.Destroy()
	waitReleased()
}
