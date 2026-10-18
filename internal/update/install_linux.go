package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Asset is the name of this system's download, or "" if there is none.
func Asset() string {
	if runtime.GOARCH != "amd64" {
		return "" // only built for Intel and AMD PCs
	}
	return "Mugig-Linux.tar.xz"
}

// location is what an update replaces: the mugig program. It says why when
// it can't be replaced, as when installed for every user, which only an
// administrator can change.
func location() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if filepath.Base(exe) != "mugig" || strings.Contains(exe, "/go-build") {
		return "", ErrFromSource
	}
	return exe, nil
}

// cantChange says that Mugig can't change dir, where it is installed.
func cantChange(dir string) error {
	return fmt.Errorf("Mugig is installed for every user, in %s, which only an administrator can change", dir)
}

// Install puts the program in the downloaded archive at pkg in place of
// the one at target. The archive is xz-compressed, which tar unpacks.
func Install(pkg, target string) error {
	dir, err := workDir(target)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) // with the old program in it, once replaced
	const inside = "mugig/usr/local/bin/mugig"
	if out, err := exec.Command("tar", "-xJf", pkg, "-C", dir, inside).CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't unpack the download: %s", strings.TrimSpace(string(out)))
	}
	next := filepath.Join(dir, inside)
	if fi, err := os.Stat(next); err != nil || !fi.Mode().IsRegular() {
		return errors.New("the download has no mugig program in it")
	}
	if err := os.Chmod(next, 0o755); err != nil {
		return err
	}
	return swap(target, next, filepath.Join(dir, "old"))
}

// Restart starts the program at target, which waits for this copy to quit.
func Restart(target string) error {
	cmd := exec.Command(target)
	cmd.Env = restartEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start the new version: %w", err)
	}
	return cmd.Process.Release()
}
