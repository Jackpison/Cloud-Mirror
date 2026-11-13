//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a console window flashing up for every rclone call.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
