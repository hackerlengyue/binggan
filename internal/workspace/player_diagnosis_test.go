package workspace

import (
	"errors"
	"fmt"
	"testing"

	"time.haomen/binggan/v2/internal/engine"
)

func TestPlayerDiagnosisSeparatesInstallationFromUnverifiedParameters(t *testing.T) {
	unavailable := &engine.PlayerParametersUnavailable{Installation: engine.Installation{Path: `E:\SZPlayer 26.10.553\MainPlayer.exe`, Version: "26.10.553"}}
	for _, err := range []error{unavailable, fmt.Errorf("discovery: %w", unavailable)} {
		player, checks := diagnosePlayer(engine.Player{}, err)
		if player != nil || len(checks) != 2 {
			t.Fatalf("unverified parameters must not invent a player identity: %+v, %+v", player, checks)
		}
		if !checks[0].OK || checks[0].Warning || checks[0].Name != "播放器安装位置" || checks[0].Message != unavailable.Installation.Path+" · 26.10.553" {
			t.Fatalf("installation should pass: %+v", checks[0])
		}
		if checks[1].OK || !checks[1].Warning || checks[1].Name != "播放器解密参数" || checks[1].Message != err.Error() {
			t.Fatalf("unverified parameters should remain a configuration warning: %+v", checks[1])
		}
	}
}

func TestPlayerDiagnosisKeepsRealFailuresAndVerifiedSuccess(t *testing.T) {
	failure := errors.New("player not found")
	player, checks := diagnosePlayer(engine.Player{}, failure)
	if player != nil || len(checks) != 1 || checks[0].OK || checks[0].Warning || checks[0].Message != failure.Error() {
		t.Fatalf("missing player must remain an error: %+v, %+v", player, checks)
	}
	want := engine.Player{SoftwareName: "MAC_sz_fixture", AppMD5: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	player, checks = diagnosePlayer(want, nil)
	if player == nil || *player != want || len(checks) != 1 || !checks[0].OK || checks[0].Warning || checks[0].Message != want.SoftwareName {
		t.Fatalf("verified player should pass: %+v, %+v", player, checks)
	}
}
