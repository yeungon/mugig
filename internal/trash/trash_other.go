//go:build !darwin && !windows

package trash

import (
	"errors"
	"os/exec"
	"strings"
)

// move uses the desktop's trash command: gio (GNOME and most others), or
// trash-cli's trash-put. Both follow the freedesktop.org Trash
// specification, so files can be put back from the file manager.
func move(path string) error {
	for _, cmd := range [][]string{{"gio", "trash", "--"}, {"trash-put", "--"}} {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			continue
		}
		out, err := exec.Command(cmd[0], append(cmd[1:], path)...).CombinedOutput()
		if err != nil {
			if msg := strings.TrimSpace(string(out)); msg != "" {
				return errors.New(msg)
			}
			return err
		}
		return nil
	}
	return errors.New("no trash command found (install gio or trash-cli)")
}
