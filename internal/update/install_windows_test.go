package update

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipExe(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Mugig-Windows.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create(name)
	w.Write([]byte(body))
	z.Close()
	f.Close()
	return path
}

func TestInstallWindows(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "Mugig.exe"), "old")
	if err := Install(zipExe(t, "Mugig.exe", "new"), target); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new" {
		t.Error("not installed")
	}
	// The old one waits next to it, for the next start to delete.
	left, _ := filepath.Glob(filepath.Join(dir, tempPrefix+"*", "old.exe"))
	if len(left) != 1 || readFile(t, left[0]) != "old" {
		t.Errorf("old copy: %v", left)
	}
}

func TestInstallWindowsNoExe(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "Mugig.exe"), "old")
	if err := Install(zipExe(t, "Other.exe", "new"), target); err == nil || !strings.Contains(err.Error(), "no Mugig.exe") {
		t.Errorf("got %v", err)
	}
	if readFile(t, target) != "old" {
		t.Error("target changed")
	}
}
