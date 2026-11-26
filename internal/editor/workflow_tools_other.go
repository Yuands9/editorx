//go:build !windows

package editor

import "os/exec"

func configureCustomTool(command *exec.Cmd) {}
