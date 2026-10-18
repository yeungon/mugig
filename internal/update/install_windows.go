package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
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
	return "Mugig-Windows.zip"
}

// location is what an update replaces: Mugig.exe. It says why when it can't
// be replaced.
func location() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if strings.Contains(exe, `\go-build`) { // go run's
		return "", ErrFromSource
	}
	return exe, nil
}

// cantChange says that Mugig can't change dir, where it is installed.
func cantChange(dir string) error {
	return fmt.Errorf("Mugig isn't allowed to change %s", dir)
}

// Install puts the Mugig.exe in the downloaded zip at pkg in place of the
// one at target. Windows lets a running program be renamed, though not
// deleted, so the old one is moved aside, for AfterRestart to delete.
func Install(pkg, target string) error {
	dir, err := workDir(target)
	if err != nil {
		return err
	}
	next := filepath.Join(dir, "Mugig.exe")
	if err := unzipOne(pkg, "Mugig.exe", next); err != nil {
		return err
	}
	if err := swap(target, next, filepath.Join(dir, "old.exe")); err != nil {
		os.RemoveAll(dir)
		return err
	}
	return nil
}

// unzipOne copies the file called name out of the zip at pkg, to path.
func unzipOne(pkg, name, path string) error {
	z, err := zip.OpenReader(pkg)
	if err != nil {
		return fmt.Errorf("couldn't unzip the download: %w", err)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		w, err := os.Create(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, io.LimitReader(r, maxDownload)); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	return errors.New("the download has no " + name + " in it")
}

// Restart starts the Mugig.exe at target, which waits for this copy to
// quit.
func Restart(target string) error {
	cmd := exec.Command(target)
	cmd.Env = restartEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("couldn't start the new version: %w", err)
	}
	return cmd.Process.Release()
}

// running reports whether the process numbered pid is still running.
func running(pid int) bool {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	r, _ := syscall.WaitForSingleObject(h, 0)
	return r == syscall.WAIT_TIMEOUT
}
