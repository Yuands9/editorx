//go:build !windows

package editor

import "os"

func openSharedTextFile(path string) (*os.File, error) { return os.Open(path) }
