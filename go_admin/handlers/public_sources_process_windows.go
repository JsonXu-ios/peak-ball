package handlers

import (
	"os/exec"
	"syscall"
)

func hidePublicSourceProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
