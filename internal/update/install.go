package update

// Putting a downloaded version in place of the one running, and starting
// it. What is replaced, and how, depends on the system (install_*.go):
// the app on macOS, Mugig.exe on Windows, the program on Linux. Each is
// replaced by renaming, so it is either the old version or the new, never
// half of one, and the running copy carries on from the old files until
// it quits.
//
// The new copy is started before the old one quits, and told to wait for
// it (AfterRestart), so they never use the settings at the same time.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// waitEnv tells a copy started by Restart which copy to wait for.
const waitEnv = "MUGIG_WAIT_PID"

// tempPrefix starts the names of the folders an update works in, next to
// what it replaces; any left by an update cut short are removed later.
const tempPrefix = ".mugig-update-"

// ErrFromSource is returned by Target for a copy that isn't installed
// from a release, as when run with go run.
var ErrFromSource = errors.New("this copy of Mugig wasn't installed from a release")

// Target is what an update replaces: the app on macOS, Mugig.exe on
// Windows, the program on Linux. It says why when that can't be replaced.
func Target() (string, error) {
	t, err := location()
	if err != nil {
		return "", err
	}
	if writable(filepath.Dir(t)) != nil {
		return "", cantChange(filepath.Dir(t))
	}
	return t, nil
}

// swap puts next in place of target, moving target to old, and back if
// next can't be put there.
func swap(target, next, old string) error {
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(next, target); err != nil {
		os.Rename(old, target)
		return err
	}
	return nil
}

// writable checks that files can be made in dir.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, tempPrefix+"test-")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// workDir makes a folder to work in next to target, on the same disk, so
// files can be renamed from it into target's place.
func workDir(target string) (string, error) {
	return os.MkdirTemp(filepath.Dir(target), tempPrefix)
}

// AfterRestart is called first thing when Mugig starts. If it was started
// by Restart, it waits for the copy it replaces to quit. It also removes
// what earlier updates left behind, such as the old Mugig.exe, which
// Windows doesn't let a running copy delete.
func AfterRestart() {
	if s := os.Getenv(waitEnv); s != "" {
		os.Unsetenv(waitEnv)
		if pid, err := strconv.Atoi(s); err == nil {
			for end := time.Now().Add(15 * time.Second); running(pid) && time.Now().Before(end); {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	if target, err := location(); err == nil {
		left, _ := filepath.Glob(filepath.Join(filepath.Dir(target), tempPrefix+"*"))
		for _, dir := range left {
			os.RemoveAll(dir)
		}
	}
}

// restartEnv is the environment for the copy Restart starts.
func restartEnv() []string {
	return append(os.Environ(), waitEnv+"="+strconv.Itoa(os.Getpid()))
}
