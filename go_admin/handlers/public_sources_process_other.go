//go:build !windows

package handlers

import "os/exec"

func hidePublicSourceProcess(cmd *exec.Cmd) {}
