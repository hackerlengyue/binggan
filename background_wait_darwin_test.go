//go:build darwin

package main

import (
	"github.com/egoist/mygo"
	"os"
	"testing"
	"time"
)

// Exercise the real generated service transport in a hidden WKWebView. No
// SzPlayer, capture, foreground activation or external input is involved.
func TestMyGoBackgroundPlayerWait(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") != "1" {
		t.Skip("native WKWebView test")
	}
	w := mygo.NewWindow(mygo.WindowOptions{Title: "Background orchestration check", URL: "/", Width: 400, Height: 300})
	defer w.Close()
	time.Sleep(time.Second)
	_, err := mygo.EvalAs[bool](w.Page(), `(()=>{globalThis.playerWaitProbe={ticks:0,error:"",done:false};(async()=>{try{while(!playerWaitProbe.done){await window.mygo.call('PlayerService.Wait',200);playerWaitProbe.ticks++;}}catch(error){playerWaitProbe.error=String(error);}})();return true})()`)
	if err != nil {
		t.Fatal(err)
	}
	w.Hide()
	// Deliberately do not evaluate JS while hidden: eval itself could wake it.
	time.Sleep(5 * time.Second)
	result, err := mygo.EvalAs[struct {
		Ticks  int    `json:"ticks"`
		Error  string `json:"error"`
		Hidden bool   `json:"hidden"`
	}](w.Page(), `(()=>{playerWaitProbe.done=true;return {...playerWaitProbe,hidden:document.hidden};})()`)
	if err != nil || result.Error != "" || !result.Hidden || result.Ticks < 15 {
		t.Fatalf("hidden orchestration timer failed: %+v %v", result, err)
	}
	t.Logf("hidden WKWebView completed %d native waits in 5 seconds", result.Ticks)
}
