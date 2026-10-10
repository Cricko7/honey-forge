//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {}

func isolateChild(cmd *exec.Cmd) {
	if os.Geteuid() == 0 && os.Getenv("AGENT_TOKEN_FILE") != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65532, Gid: 65532, NoSetGroups: true}}
	}
}
