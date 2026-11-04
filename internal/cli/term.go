package cli

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// terminalWidth returns the width of the terminal the picker is running in.
// The popup owns /dev/tty, so stty reports the popup size, not the outer
// terminal. Zero means unknown and the renderer falls back to fixed widths.
func terminalWidth() int {
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return 0
	}
	defer tty.Close()

	cmd := exec.Command("stty", "size")
	cmd.Stdin = tty
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0
	}
	cols, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return cols
}
