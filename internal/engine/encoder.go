package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Prefer the platform AAC implementation on macOS. Other platforms keep the
// FFmpeg encoder. Explicit configuration is useful for repeatable comparisons.
func audioEncoder(ctx context.Context) (name string, fallback bool, err error) {
	preference := strings.ToLower(strings.TrimSpace(os.Getenv("SZJM_AAC_ENCODER")))
	if preference == "aac" {
		return "aac", false, nil
	}
	if preference == "aac_at" {
		return "aac_at", false, nil
	}
	if preference != "" && preference != "auto" {
		return "", false, fmt.Errorf("SZJM_AAC_ENCODER 仅支持 auto、aac、aac_at")
	}
	if runtime.GOOS == "darwin" {
		encoders, e := output(ctx, "ffmpeg", nil, "-hide_banner", "-encoders")
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if e == nil && hasEncoder(string(encoders), "aac_at") {
			return "aac_at", true, nil
		}
	}
	return "aac", false, nil
}

func hasEncoder(list, name string) bool {
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name && strings.HasPrefix(fields[0], "A") {
			return true
		}
	}
	return false
}
