package workspace

import (
	"context"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"

	"time.haomen/binggan/v2/internal/procutil"
)

func commandOutput(ctx context.Context, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	procutil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// architectureName also says whether an Intel build is being translated by
// Rosetta, which explains slow ffmpeg runs on Apple Silicon.
func architectureName(ctx context.Context) string {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "arm64 · Apple Silicon"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		if commandOutput(ctx, "sysctl", "-n", "sysctl.proc_translated") == "1" {
			return "amd64 · Rosetta 转译"
		}
		return "amd64 · Intel"
	case runtime.GOARCH == "amd64":
		return "amd64 · x64"
	}
	return runtime.GOARCH
}

// moduleVersion reads a dependency version recorded in the executable.
func moduleVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range bi.Deps {
		if dep.Path != path {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		return dep.Version
	}
	return ""
}

func singBoxVersion() string {
	if version := moduleVersion("github.com/sagernet/sing-box"); version != "" {
		return version
	}
	return "未知"
}
