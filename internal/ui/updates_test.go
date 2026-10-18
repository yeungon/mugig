package ui

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"mugig/internal/update"
)

// fakeGitHub stands in for asking GitHub for the newest release.
type fakeGitHub struct {
	newest update.Release
	err    error
	asked  int
	opened []string // pages opened in the browser
}

// newUpdateUI makes a UI that is version 0.1.0, asking a fake GitHub.
func newUpdateUI(t *testing.T, newest string) (*ui, *fakeGitHub) {
	t.Helper()
	old := appMetadata
	t.Cleanup(func() { appMetadata = old })
	appMetadata = func() fyne.AppMetadata {
		return fyne.AppMetadata{Name: "Mugig", Version: "0.1.0", Build: 7, Release: true}
	}
	u, _, _ := newTestUI(t)
	g := &fakeGitHub{newest: update.Release{Version: newest, Page: "https://github.com/yeungon/mugig/releases/tag/v" + newest}}
	u.latestRelease = func(context.Context) (update.Release, error) {
		g.asked++
		return g.newest, g.err
	}
	u.openURL = func(l *url.URL) error {
		g.opened = append(g.opened, l.String())
		return nil
	}
	u.updater = updater{asset: "Mugig-macOS.zip", target: func() (string, error) { return "", update.ErrFromSource }}
	u.quit = func() { t.Error("quit") }
	return u, g
}

// fakeInstall stands in for downloading, installing and restarting.
type fakeInstall struct {
	downloaded                          []string // the downloads fetched, as release/asset
	installed                           []string // what was installed, as package -> target
	restarted                           []string
	quit                                bool
	downloadErr, installErr, restartErr error
	during                              func() // runs while downloading
}

// canInstall lets the UI install updates in place of /Applications/Mugig.app.
func canInstall(u *ui) *fakeInstall {
	f := &fakeInstall{}
	u.updater = updater{
		asset:  "Mugig-macOS.zip",
		target: func() (string, error) { return "/Applications/Mugig.app", nil },
		download: func(ctx context.Context, r update.Release, asset, dir string, progress func(int64, int64)) (string, error) {
			f.downloaded = append(f.downloaded, r.Version+"/"+asset)
			progress(5<<20, 20<<20)
			if f.during != nil {
				f.during()
			}
			if f.downloadErr != nil {
				return "", f.downloadErr
			}
			return dir + "/" + asset, ctx.Err()
		},
		install: func(pkg, target string) error {
			f.installed = append(f.installed, filepath.Base(pkg)+" -> "+target)
			return f.installErr
		},
		restart: func(target string) error {
			f.restarted = append(f.restarted, target)
			return f.restartErr
		},
	}
	u.quit = func() { f.quit = true }
	return f
}

// Help > Check for Updates… says when a newer version is out, and opens
// its page; the sidebar says so too, and keeps saying it.
func TestCheckForUpdates(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	if u.updateNotice.Visible() {
		t.Error("notice shown before checking")
	}
	menuItem(t, u, "Check for Updates…").Action()
	if g.asked != 1 {
		t.Fatalf("asked GitHub %d times", g.asked)
	}
	if got := aboutTexts(u); !strings.Contains(got, "Mugig 0.2.0 is out. You have 0.1.0.") {
		t.Errorf("dialog says %q", got)
	}
	if !u.updateNotice.Visible() || u.updateNotice.Text != "Mugig 0.2.0 is out" {
		t.Errorf("notice %q, shown %v", u.updateNotice.Text, u.updateNotice.Visible())
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Open Its Page"))
	if len(g.opened) != 1 || g.opened[0] != "https://github.com/yeungon/mugig/releases/tag/v0.2.0" {
		t.Errorf("opened %v", g.opened)
	}

	// The notice opens the same.
	test.Tap(u.updateNotice)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Later"))
	test.Tap(u.updateNotice)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Open Its Page"))
	if len(g.opened) != 2 {
		t.Errorf("opened %v", g.opened)
	}

	// Started again, the notice is there before checking.
	u.buildUpdateNotice()
	if !u.updateNotice.Visible() || u.updateNotice.Text != "Mugig 0.2.0 is out" {
		t.Errorf("after a restart: notice %q, shown %v", u.updateNotice.Text, u.updateNotice.Visible())
	}
}

func TestUpToDate(t *testing.T) {
	u, _ := newUpdateUI(t, "0.1.0")
	menuItem(t, u, "Check for Updates…").Action()
	if got := aboutTexts(u); !strings.Contains(got, "Mugig 0.1.0 is the newest version.") {
		t.Errorf("dialog says %q", got)
	}
	if u.updateNotice.Visible() {
		t.Error("notice shown")
	}
	// Once updated, what was found before isn't shown.
	u.buildUpdateNotice()
	if u.updateNotice.Visible() {
		t.Error("notice shown after a restart")
	}
}

func TestCheckForUpdatesOffline(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	g.err = errors.New("dial tcp: lookup github.com: no such host")
	menuItem(t, u, "Check for Updates…").Action()
	if got := aboutTexts(u); !strings.Contains(got, "Check that you're online") {
		t.Errorf("dialog says %q", got)
	}
	if u.updateNotice.Visible() || u.prefs.Int(prefUpdateChecked) != 0 {
		t.Error("an unanswered check counted")
	}
}

// Without a version, as when built from source, the newest is named but
// not compared.
func TestCheckForUpdatesUnversioned(t *testing.T) {
	u, _ := newUpdateUI(t, "0.2.0")
	appMetadata = func() fyne.AppMetadata { return fyne.AppMetadata{} }
	menuItem(t, u, "Check for Updates…").Action()
	if got := aboutTexts(u); !strings.Contains(got, "Mugig 0.2.0 Is the Newest Version") || !strings.Contains(got, "built from source") {
		t.Errorf("dialog says %q", got)
	}
}

// The daily check asks once a day, quietly, unless turned off.
func TestDailyUpdateCheck(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	auto := menuItem(t, u, "Check for Updates Automatically")
	if !auto.Checked {
		t.Fatal("not on to begin with")
	}
	now := time.Now()
	u.checkIfDue(now)
	if g.asked != 1 || !u.updateNotice.Visible() {
		t.Fatalf("asked %d times, notice shown %v", g.asked, u.updateNotice.Visible())
	}
	if u.win.Canvas().Overlays().Top() != nil {
		t.Error("the daily check opened a dialog")
	}
	u.checkIfDue(now.Add(23 * time.Hour))
	if g.asked != 1 {
		t.Error("asked again within a day")
	}
	u.checkIfDue(now.Add(25 * time.Hour))
	if g.asked != 2 {
		t.Error("not asked again after a day")
	}

	auto.Action()
	if auto.Checked || u.prefs.BoolWithFallback(prefCheckUpdates, true) {
		t.Error("not turned off")
	}
	u.checkIfDue(now.Add(100 * time.Hour))
	if g.asked != 2 {
		t.Error("asked while turned off")
	}
	// Asking from the menu still works.
	menuItem(t, u, "Check for Updates…").Action()
	if g.asked != 3 {
		t.Error("Check for Updates… didn't ask")
	}
}

// A failed daily check is tried again at the next chance, not a day later.
func TestDailyUpdateCheckRetries(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	g.err = errors.New("offline")
	now := time.Now()
	u.checkIfDue(now)
	if u.updateNotice.Visible() || u.win.Canvas().Overlays().Top() != nil {
		t.Error("a failed daily check showed something")
	}
	g.err = nil
	u.checkIfDue(now.Add(time.Hour))
	if g.asked != 2 || !u.updateNotice.Visible() {
		t.Errorf("asked %d times, notice shown %v", g.asked, u.updateNotice.Visible())
	}
}

// Copies built from source don't check by themselves.
func TestNoDailyCheckFromSource(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	appMetadata = func() fyne.AppMetadata { return fyne.AppMetadata{Name: "Mugig", Version: "0.1.0"} }
	first := updateFirstWait
	updateFirstWait = time.Millisecond
	t.Cleanup(func() { updateFirstWait = first })
	u.startUpdateChecks()
	time.Sleep(50 * time.Millisecond)
	if g.asked != 0 {
		t.Error("checked")
	}
}

// Where it can, Mugig installs the update itself, and restarts.
func TestUpdateNow(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	f := canInstall(u)
	var during string
	f.during = func() { during = aboutTexts(u) }
	menuItem(t, u, "Check for Updates…").Action()
	got := aboutTexts(u)
	for _, want := range []string{"Mugig 0.2.0 is out. You have 0.1.0.", "puts it in place of this version",
		"What's new in 0.2.0 <https://github.com/yeungon/mugig/releases/tag/v0.2.0>"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Update Now"))
	if !strings.Contains(during, "Downloading Mugig 0.2.0… 5.24 MB of 21.0 MB") {
		t.Errorf("while downloading: %q", during)
	}
	if strings.Join(f.downloaded, ",") != "0.2.0/Mugig-macOS.zip" ||
		strings.Join(f.installed, ",") != "Mugig-macOS.zip -> /Applications/Mugig.app" ||
		strings.Join(f.restarted, ",") != "/Applications/Mugig.app" || !f.quit {
		t.Errorf("downloaded %v, installed %v, restarted %v, quit %v", f.downloaded, f.installed, f.restarted, f.quit)
	}
	if len(g.opened) != 0 {
		t.Errorf("opened %v", g.opened)
	}
}

// A failed update says why, and offers the page instead.
func TestUpdateFails(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	f := canInstall(u)
	f.downloadErr = update.ErrBadSignature
	u.showUpdate(g.newest)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Update Now"))
	if got := aboutTexts(u); !strings.Contains(got, "Mugig 0.2.0 couldn't be installed: the download's signature doesn't match") {
		t.Errorf("says %q", got)
	}
	if len(f.installed) != 0 || f.quit {
		t.Error("installed a download that failed its check")
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Open Its Page"))
	if len(g.opened) != 1 {
		t.Errorf("opened %v", g.opened)
	}

	// Offline, it says so plainly.
	f.downloadErr = &url.Error{Op: "Get", URL: "https://github.com/…", Err: errors.New("dial tcp: no such host")}
	u.showUpdate(g.newest)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Update Now"))
	if got := aboutTexts(u); !strings.Contains(got, "the download didn't finish. Check that you're online") || strings.Contains(got, "dial tcp") {
		t.Errorf("says %q", got)
	}
}

// The download can be cancelled.
func TestCancelUpdate(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	f := canInstall(u)
	f.during = func() { test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Cancel")) }
	u.showUpdate(g.newest)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Update Now"))
	if len(f.installed) != 0 || f.quit {
		t.Error("installed after cancelling")
	}
	if top := u.win.Canvas().Overlays().Top(); top != nil {
		t.Errorf("left %q showing", aboutTexts(u))
	}
}

// Installed, but not restarted, it says to restart.
func TestUpdateRestartFails(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	f := canInstall(u)
	f.restartErr = errors.New("no")
	u.showUpdate(g.newest)
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Update Now"))
	if got := aboutTexts(u); !strings.Contains(got, "Quit Mugig and open it again") || f.quit {
		t.Errorf("says %q, quit %v", got, f.quit)
	}
}

// Where Mugig can't replace itself, it says why, and offers the page.
func TestCantUpdateHere(t *testing.T) {
	u, g := newUpdateUI(t, "0.2.0")
	u.updater.target = func() (string, error) { return "", errors.New("Mugig isn't allowed to change /Applications") }
	u.showUpdate(g.newest)
	got := aboutTexts(u)
	if !strings.Contains(got, "Mugig can't update itself here: Mugig isn't allowed to change /Applications.") || strings.Contains(got, "What's new") {
		t.Errorf("says %q", got)
	}
	test.Tap(findButton(t, u.win.Canvas().Overlays().Top(), "Open Its Page"))
	if len(g.opened) != 1 {
		t.Errorf("opened %v", g.opened)
	}
	// Built from source, or without a download for this system, it just
	// offers the page.
	for _, c := range []updater{
		{asset: "Mugig-macOS.zip", target: func() (string, error) { return "", update.ErrFromSource }},
		{asset: "", target: func() (string, error) { return "/usr/bin/mugig", nil }},
	} {
		u.updater = c
		u.showUpdate(g.newest)
		if got := aboutTexts(u); strings.Contains(got, "can't update itself") || !strings.Contains(got, "has the download for your computer") {
			t.Errorf("says %q", got)
		}
		findButton(t, u.win.Canvas().Overlays().Top(), "Open Its Page")
	}
}
