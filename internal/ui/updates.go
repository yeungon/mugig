package ui

// Telling the user when a newer Mugig is out. Once a day, released copies
// ask GitHub which version is newest (see package update); if it is newer
// than this one, a button at the foot of the sidebar says so, and opens
// the release's page, with what's new and the downloads. Help > Check for
// Updates… asks at once, and Help > Check for Updates Automatically turns
// the daily check off. Copies built from source (go run) don't check by
// themselves, as their version isn't a released one.

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mugig/internal/update"
)

const (
	prefCheckUpdates  = "checkForUpdates"   // check once a day; on unless turned off
	prefUpdateChecked = "updateCheckedAt"   // when GitHub last answered, in Unix seconds
	prefNewestVersion = "newestVersion"     // the newest release GitHub named
	prefNewestPage    = "newestReleasePage" // and its page
)

var (
	updateEvery     = 24 * time.Hour
	updateFirstWait = 10 * time.Second // after launch, so it doesn't slow it
	updatePoll      = time.Hour        // how often to see whether a check is due
)

// buildUpdateItems creates Help > Check for Updates… and Check for Updates
// Automatically.
func (u *ui) buildUpdateItems() []*fyne.MenuItem {
	u.autoUpdateItem = fyne.NewMenuItem("Check for Updates Automatically", func() {
		u.autoUpdateItem.Checked = !u.autoUpdateItem.Checked
		u.prefs.SetBool(prefCheckUpdates, u.autoUpdateItem.Checked)
		u.mainMenu.Refresh()
	})
	u.autoUpdateItem.Checked = u.prefs.BoolWithFallback(prefCheckUpdates, true)
	return []*fyne.MenuItem{fyne.NewMenuItem("Check for Updates…", u.checkForUpdates), u.autoUpdateItem}
}

// buildUpdateNotice creates the button saying a newer version is out,
// hidden until one is, or straight away if one was found before.
func (u *ui) buildUpdateNotice() *widget.Button {
	u.updateNotice = widget.NewButtonWithIcon("", theme.DownloadIcon(), nil)
	u.updateNotice.Alignment = widget.ButtonAlignLeading
	u.updateNotice.Importance = widget.HighImportance
	u.updateNotice.Hide()
	if v := u.prefs.String(prefNewestVersion); update.Newer(v, appMetadata().Version) {
		u.showUpdateNotice(update.Release{Version: v, Page: u.prefs.String(prefNewestPage)})
	}
	return u.updateNotice
}

// startUpdateChecks checks for a newer version now and then, while the
// app runs, if this is a released copy.
func (u *ui) startUpdateChecks() {
	if !appMetadata().Release {
		return
	}
	go func() {
		time.Sleep(updateFirstWait)
		for {
			onUI(func() { u.checkIfDue(time.Now()) })
			time.Sleep(updatePoll)
		}
	}()
}

// checkIfDue asks GitHub for the newest version, unless it was asked less
// than a day before now, or the user turned the daily check off.
func (u *ui) checkIfDue(now time.Time) {
	if !u.autoUpdateItem.Checked {
		return
	}
	if last := time.Unix(int64(u.prefs.Int(prefUpdateChecked)), 0); now.Sub(last) < updateEvery {
		return
	}
	u.findNewest(func(r update.Release, err error) {
		if err == nil && update.Newer(r.Version, appMetadata().Version) {
			u.showUpdateNotice(r)
		}
	})
}

// checkForUpdates asks GitHub for the newest version, and says what it
// answered.
func (u *ui) checkForUpdates() {
	u.findNewest(func(r update.Release, err error) {
		current := appMetadata().Version
		switch {
		case err != nil:
			dialog.ShowInformation("Couldn't Check for Updates",
				"Mugig couldn't find out from GitHub, where it is released. Check that you're online, then try again.", u.win)
		case update.Newer(r.Version, current):
			u.showUpdateNotice(r)
			u.showUpdate(r)
		case current == "":
			dialog.ShowInformation("Mugig "+r.Version+" Is the Newest Version",
				"This copy of Mugig has no version number, as it was built from source, so it can't tell whether it is older.", u.win)
		default:
			dialog.ShowInformation("You Have the Newest Version",
				fmt.Sprintf("Mugig %s is the newest version.", current), u.win)
		}
	})
}

// findNewest asks GitHub for the newest version, off the UI thread, and
// hands over the answer on it, remembering a good one.
func (u *ui) findNewest(then func(update.Release, error)) {
	u.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r, err := u.latestRelease(ctx)
		onUI(func() {
			if err == nil {
				u.prefs.SetInt(prefUpdateChecked, int(time.Now().Unix()))
				u.prefs.SetString(prefNewestVersion, r.Version)
				u.prefs.SetString(prefNewestPage, r.Page)
			}
			then(r, err)
		})
	})
}

// showUpdateNotice shows the button saying release r is out.
func (u *ui) showUpdateNotice(r update.Release) {
	u.updateNotice.SetText(fmt.Sprintf("Mugig %s is out", r.Version))
	u.updateNotice.OnTapped = func() { u.showUpdate(r) }
	u.updateNotice.Show()
	if u.sidebarFoot != nil { // nil while the sidebar is being built
		u.sidebarFoot.Refresh() // to make room for it
	}
}

// showUpdate says release r is out, offering to open its page.
func (u *ui) showUpdate(r update.Release) {
	text := fmt.Sprintf("Mugig %s is out. You have %s.\n\nIts page says what's new, and has the download for your computer.",
		r.Version, appMetadata().Version)
	msg := widget.NewLabel(text)
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("A New Version of Mugig", "Open Its Page", "Later", msg, func(open bool) {
		if !open {
			return
		}
		if page, err := url.Parse(r.Page); err == nil && r.Page != "" {
			if err := u.openURL(page); err != nil {
				dialog.ShowError(err, u.win)
			}
		}
	}, u.win)
	d.Resize(fyne.NewSize(aboutWidth, 0))
	d.Show()
}
