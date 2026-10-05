package tunnel

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	boxlog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/route/rule"
)

// The vendor's Windows distribution contains MainPlayer.exe, not SzPlayer.exe.
// Exercise the actual serialized route, without opening a TUN or network socket.
func TestWindowsDistributionPlayerRoute(t *testing.T) {
	ctx, opts, err := Options(descriptor(t))
	if err != nil {
		t.Fatal(err)
	}
	var matchers []adapter.Rule
	for _, r := range opts.Route.Rules {
		if r.DefaultOptions.Action != "route" || r.DefaultOptions.RouteOptions.Outbound != "capture" {
			continue
		}
		m, err := rule.NewRule(ctx, boxlog.NewNOPFactory().Logger(), r, true)
		if err != nil {
			t.Fatal(err)
		}
		matchers = append(matchers, m)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{`C:\Users\test\Downloads\SZPlayer 24.12.531\MainPlayer.exe`, true},
		{`D:\学习软件\SZPlayer 26.06.551\MainPlayer.exe`, true},
		{`C:\Program Files\SZPlayer\mainplayer.EXE`, true},
		{`C:\Program Files\OtherPlayer\MainPlayer.exe`, false},
		{`C:\Users\test\Downloads\NotSZPlayer\MainPlayer.exe`, false},
		{`C:\Users\test\Downloads\SZPlayer 26.06.551\UpDater.exe`, false},
		{`C:\Users\test\Downloads\SZPlayer 26.06.551\download\chatroom.exe`, false},
		{`C:\Program Files\Chrome\chrome.exe`, false},
		{`C:\SZPlayer\MainPlayer.exe.backup`, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			metadata := adapter.InboundContext{ProcessInfo: &adapter.ConnectionOwner{ProcessPath: tc.path}}
			got := false
			for _, m := range matchers {
				got = got || m.Match(&metadata)
			}
			if got != tc.want {
				t.Fatalf("capture = %v, want %v", got, tc.want)
			}
		})
	}
}
