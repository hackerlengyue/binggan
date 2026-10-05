package playerautomation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFrontendClickArgumentsPassNativeValidation(t *testing.T) {
	frontend := filepath.Join("..", "..", "frontend")
	tsx := filepath.Join(frontend, "node_modules", "tsx")
	if _, err := os.Stat(tsx); err != nil {
		t.Skip("install frontend dependencies to run the TypeScript-to-Go contract test")
	}
	for _, source := range []string{"src/lib/player-orchestration.ts", "src/lib/player-automation.ts", "src/mygo.ts", "tests/fixtures/player-click-contract.ts", "tests/mygo-fixture.ts"} {
		if _, err := os.ReadFile(filepath.Join(frontend, source)); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("node", "--import", "tsx", "tests/fixtures/player-click-contract.ts")
	command.Dir = frontend
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("frontend contract: %v\n%s", err, output)
	}
	var requests []ClickRequest
	if err = json.Unmarshal(output, &requests); err != nil {
		t.Fatalf("decode IPC: %v\n%s", err, output)
	}
	if len(requests) != 2 {
		t.Fatalf("clicks=%d want unique and duplicate-name cases", len(requests))
	}
	for _, request := range requests {
		if request.MatchCount != 0 || request.MatchOrdinal != 0 {
			t.Fatal("frontend mixed legacy and path identity")
		}
		got, err := validateClick(request)
		if err != nil {
			t.Fatalf("actual frontend IPC rejected: %v", err)
		}
		if got.ExpectedPID != 123 || len(got.Path) != 2 || got.Path[0] != "目录" || got.Path[1] != "第一讲 .sz" || got.ClickCount != 2 {
			t.Fatalf("wrong locator: %+v", got)
		}
	}
}
