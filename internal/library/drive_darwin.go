//go:build darwin

package library

import (
	"os/exec"
	"strings"
	"syscall"
)

// ScanWorkers asks macOS whether root is on a solid-state drive. USB
// enclosures and network shares usually don't say, so they are read gently,
// which costs little: tags are small, and rescans mostly use the cache.
func ScanWorkers(root string) int {
	if solidState(root) {
		return parallelWorkers()
	}
	return GentleWorkers
}

// solidState reports whether Disk Utility says path's drive is solid-state.
// diskutil wants the volume, not a folder on it, so it is given the folder's
// mount point.
func solidState(path string) bool {
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil {
		return false
	}
	mount := cString(fs.Mntonname[:])
	out, err := exec.Command("diskutil", "info", "-plist", mount).Output()
	return err == nil && plistSolidState(string(out))
}

// cString turns a NUL-terminated C string into a Go string.
func cString(b []int8) string {
	var s []byte
	for _, c := range b {
		if c == 0 {
			break
		}
		s = append(s, byte(c))
	}
	return string(s)
}

// plistSolidState reads the SolidState entry of diskutil's plist output.
// It is missing when the drive doesn't say.
func plistSolidState(plist string) bool {
	_, after, found := strings.Cut(plist, "<key>SolidState</key>")
	return found && strings.HasPrefix(strings.TrimSpace(after), "<true/>")
}
