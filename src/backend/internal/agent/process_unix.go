//go:build !windows

package agent

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
