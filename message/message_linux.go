package message

import (
	"os/exec"
	"syscall"
)

func MessageBox(title, message string) bool {
	err := exec.Command("zenity", "--question", "--title", title, "--text", message).Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.Sys().(syscall.WaitStatus).ExitStatus() == 0
		}
	}

	return false
}
