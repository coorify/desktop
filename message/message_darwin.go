package message

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func MessageBox(title, message string) bool {
	script := `set T to button returned of ` +
		`(display dialog "%s" with title "%s" buttons {"No", "Yes"} default button "Yes")`
	out, err := exec.Command("osascript", "-e", fmt.Sprintf(script, message, title)).Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.Sys().(syscall.WaitStatus).ExitStatus() == 0
		}
	}
	return strings.TrimSpace(string(out)) == "Yes"
}
