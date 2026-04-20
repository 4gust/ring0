//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

func setPgid(cmd *exec.Cmd) {}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
