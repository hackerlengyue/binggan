//go:build !windows

package main

import "os"

func replaceDownload(staged, dest string) error { return os.Rename(staged, dest) }
