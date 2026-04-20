//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// setPgid isolates the child in its own process group so that signals
// delivered to ring0 (or sibling apps) do not cascade.
func setPgid(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalGroup sends sig to the entire process group of cmd, falling back to
// the leader if the group can't be resolved.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pgid, err := syscall.Getpgid(pid); err == nil {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return
		}
	}
	_ = cmd.Process.Signal(sig)
}
