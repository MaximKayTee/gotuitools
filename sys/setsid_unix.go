//go:build unix

package sys

import (
	"os/exec"
	"syscall"
)

func setsid(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
