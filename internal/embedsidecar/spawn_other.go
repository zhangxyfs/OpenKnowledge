//go:build !windows

package embedsidecar

import "os/exec"

func hideWindow(*exec.Cmd) {}
