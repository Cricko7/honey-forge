//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {}

func isolateChild(cmd *exec.Cmd) {
	// The SSH worker is a trusted gateway: it creates separate offline sandboxes
	// through Docker. Attacker commands never execute in this process/container.
	if os.Getenv("NM_ROLE") == "ssh-high-interaction" {
		return
	}
	if os.Geteuid() == 0 && os.Getenv("NM_TOKEN_FILE") != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65532, Gid: 65532, NoSetGroups: true}}
	}
}
