//go:build !darwin

package main

import "time.haomen/binggan/v2/internal/playerautomation"

func launchPermissionFlow(_ *Workbench) error {
	return playerautomation.ErrUnsupported
}
