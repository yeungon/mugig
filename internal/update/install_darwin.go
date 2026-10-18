package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Asset is the name of this system's download, or "" if there is none.
func Asset() string { return "Mugig-macOS.zip" }

// location is what an update replaces: the app Mugig runs from, such as
// /Applications/Mugig.app. It says why when it can't be replaced.
func location() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(app) != ".app" || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return "", ErrFromSource
	}
	// Opened where it was downloaded, an app from the internet is run from
	// a hidden, read-only copy.
	if strings.Contains(app, "/AppTranslocation/") {
		return "", errors.New("macOS is running Mugig from a hidden copy, as it was opened where it was downloaded. Move Mugig to Applications, open it from there, and try again")
	}
	return app, nil
}

// cantChange says that Mugig can't change dir, where it is installed.
func cantChange(dir string) error {
	return fmt.Errorf("Mugig isn't allowed to change %s", dir)
}

// Install puts the app in the downloaded zip at pkg in place of the app
// at target, once macOS agrees its code signature is intact.
func Install(pkg, target string) error {
	dir, err := workDir(target)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) // with the old app in it, once replaced
	if out, err := exec.Command("ditto", "-x", "-k", pkg, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't unzip the download: %s", bytesOr(out, err))
	}
	next := filepath.Join(dir, "Mugig.app")
	if _, err := os.Stat(filepath.Join(next, "Contents", "MacOS", "mugig")); err != nil {
		return errors.New("the download has no Mugig app in it")
	}
	if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", next).CombinedOutput(); err != nil {
		return fmt.Errorf("the new app's code signature doesn't check out: %s", bytesOr(out, err))
	}
	return swap(target, next, filepath.Join(dir, "old.app"))
}

// Restart starts the app at target, which waits for this copy to quit.
func Restart(target string) error {
	out, err := exec.Command("open", "-n", "--env", waitEnv+"="+strconv.Itoa(os.Getpid()), target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("couldn't open the new version: %s", bytesOr(out, err))
	}
	return nil
}

// bytesOr is a command's output, or its error if it said nothing.
func bytesOr(out []byte, err error) string {
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return err.Error()
}
