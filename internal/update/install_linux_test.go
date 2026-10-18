package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tarProgram(t *testing.T, inside, body string) string {
	t.Helper()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, inside), body)
	path := filepath.Join(t.TempDir(), "Mugig-Linux.tar.xz")
	if out, err := exec.Command("tar", "-cJf", path, "-C", src, "mugig").CombinedOutput(); err != nil {
		t.Fatalf("tar: %s", out)
	}
	return path
}

func TestInstallLinux(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "mugig"), "old")
	if err := Install(tarProgram(t, "mugig/usr/local/bin/mugig", "new"), target); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new" {
		t.Error("not installed")
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", fi.Mode())
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("left %v", left)
	}
}

func TestInstallLinuxNoProgram(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "mugig"), "old")
	if err := Install(tarProgram(t, "mugig/other", "new"), target); err == nil || !strings.Contains(err.Error(), "unpack") {
		t.Errorf("got %v", err)
	}
	if readFile(t, target) != "old" {
		t.Error("target changed")
	}
}
