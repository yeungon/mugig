package ui

import (
	"context"
	"errors"
	"net/url"
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
	return u, g
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
