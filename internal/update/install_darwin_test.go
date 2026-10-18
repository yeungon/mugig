package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeApp makes a Mugig.app at dir whose program says what, signed ad hoc
// as releases are.
func fakeApp(t *testing.T, dir, what string) string {
	t.Helper()
	app := filepath.Join(dir, "Mugig.app")
	writeFile(t, filepath.Join(app, "Contents", "MacOS", "mugig"), "#!/bin/sh\necho "+what+"\n")
	writeFile(t, filepath.Join(app, "Contents", "Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleExecutable</key><string>mugig</string><key>CFBundleIdentifier</key><string>org.kiwipanel.musicscan</string></dict></plist>`)
	if out, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", app).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %s", out)
	}
	return app
}

// zipApp zips the app as the release workflow does.
func zipApp(t *testing.T, app string) string {
	t.Helper()
	zip := filepath.Join(t.TempDir(), "Mugig-macOS.zip")
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", app, zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %s", out)
	}
	return zip
}

func TestInstallMac(t *testing.T) {
	apps := t.TempDir()
	target := fakeApp(t, apps, "old")
	zip := zipApp(t, fakeApp(t, t.TempDir(), "new"))
	if err := Install(zip, target); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(target, "Contents", "MacOS", "mugig")); !strings.Contains(got, "new") {
		t.Errorf("installed %q", got)
	}
	if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", target).CombinedOutput(); err != nil {
		t.Errorf("installed app's signature: %s", out)
	}
	if left, _ := os.ReadDir(apps); len(left) != 1 {
		t.Errorf("left %v", left)
	}
}

// An app changed after it was signed isn't installed.
func TestInstallMacBrokenSignature(t *testing.T) {
	apps := t.TempDir()
	target := fakeApp(t, apps, "old")
	next := fakeApp(t, t.TempDir(), "new")
	writeFile(t, filepath.Join(next, "Contents", "MacOS", "mugig"), "#!/bin/sh\necho changed\n")
	if err := Install(zipApp(t, next), target); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("got %v", err)
	}
	if got := readFile(t, filepath.Join(target, "Contents", "MacOS", "mugig")); !strings.Contains(got, "old") {
		t.Errorf("target now %q", got)
	}
	if left, _ := os.ReadDir(apps); len(left) != 1 {
		t.Errorf("left %v", left)
	}
}

func TestInstallMacNotAnApp(t *testing.T) {
	apps := t.TempDir()
	target := fakeApp(t, apps, "old")
	other := filepath.Join(t.TempDir(), "Other")
	writeFile(t, filepath.Join(other, "readme"), "hello")
	if err := Install(zipApp(t, other), target); err == nil || !strings.Contains(err.Error(), "no Mugig app") {
		t.Errorf("got %v", err)
	}
}
