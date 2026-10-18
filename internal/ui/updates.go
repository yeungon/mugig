package ui

// Telling the user when a newer Mugig is out, and installing it. Once a
// day, released copies ask GitHub which version is newest (see package
// update); if it is newer than this one, a button at the foot of the
// sidebar says so. It offers to install it: download it, check its
// signature, put it in place of this copy, and open it. Where Mugig can't
// replace itself, as when it isn't allowed to change the folder it is in,
// it opens the release's page instead, with the downloads. Help > Check
// for Updates… asks at once, and Help > Check for Updates Automatically
// turns the daily check off. Copies built from source (go run) don't check
// by themselves, as their version isn't a released one, nor replace
// themselves.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mugig/internal/humanize"
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

// updater installs updates (see package update); tests replace its parts.
type updater struct {
	asset    string // this system's download, "" if there is none
	target   func() (string, error)
	download func(ctx context.Context, r update.Release, asset, dir string, progress func(done, total int64)) (string, error)
	install  func(pkg, target string) error
	restart  func(target string) error
}

var systemUpdater = updater{update.Asset(), update.Target, update.Download, update.Install, update.Restart}

// showUpdate says release r is out, offering to install it, or, where
// Mugig can't, to open its page.
func (u *ui) showUpdate(r update.Release) {
	text := fmt.Sprintf("Mugig %s is out. You have %s.", r.Version, appMetadata().Version)
	target, why := u.updater.target()
	if u.updater.asset == "" {
		why = update.ErrFromSource // nothing to update to; nothing to say
	}
	if why == nil {
		text += "\n\nUpdating downloads it, puts it in place of this version, and opens it. Your library, playlists and settings stay as they are."
	} else {
		text += "\n\nIts page says what's new, and has the download for your computer."
		if !errors.Is(why, update.ErrFromSource) {
			text += "\n\nMugig can't update itself here: " + why.Error() + "."
		}
	}
	msg := widget.NewLabel(text)
	msg.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(msg)
	confirm := "Open Its Page"
	if why == nil {
		confirm = "Update Now"
		if page, err := url.Parse(r.Page); err == nil {
			content.Add(widget.NewHyperlink("What's new in "+r.Version, page))
		}
	}
	d := dialog.NewCustomConfirm("A New Version of Mugig", confirm, "Later", content, func(ok bool) {
		switch {
		case !ok:
		case why == nil:
			u.installUpdate(r, target)
		default:
			u.openPage(r)
		}
	}, u.win)
	d.Resize(fyne.NewSize(aboutWidth, 0))
	d.Show()
}

// openPage opens release r's page in the browser.
func (u *ui) openPage(r update.Release) {
	if page, err := url.Parse(r.Page); err == nil && r.Page != "" {
		if err := u.openURL(page); err != nil {
			dialog.ShowError(err, u.win)
		}
	}
}

// installUpdate downloads release r, puts it in place of target, and
// restarts Mugig, showing how far it has got; the download can be
// cancelled, though not the installing, which takes a moment.
func (u *ui) installUpdate(r update.Release, target string) {
	ctx, cancel := context.WithCancel(context.Background())
	label := widget.NewLabel(fmt.Sprintf("Downloading Mugig %s…", r.Version))
	bar := widget.NewProgressBar()
	stop := widget.NewButton("Cancel", cancel)
	d := dialog.NewCustomWithoutButtons("Updating Mugig", container.NewVBox(label, bar, container.NewCenter(stop)), u.win)
	d.Resize(fyne.NewSize(aboutWidth, 0))
	d.Show()
	u.async(func() {
		defer cancel()
		installed, err := u.fetchAndInstall(ctx, r, target, func(done, total int64) {
			onUI(func() {
				if total > 0 {
					bar.SetValue(float64(done) / float64(total))
					label.SetText(fmt.Sprintf("Downloading Mugig %s… %s of %s", r.Version, humanize.FileSize(done), humanize.FileSize(total)))
				}
			})
		}, func() {
			onUI(func() {
				label.SetText(fmt.Sprintf("Installing Mugig %s…", r.Version))
				stop.Disable()
			})
		})
		if installed {
			err = u.updater.restart(target)
		}
		onUI(func() {
			d.Hide()
			switch {
			case installed && err != nil:
				dialog.ShowInformation("Mugig "+r.Version+" Is Installed",
					"Quit Mugig and open it again to use the new version.", u.win)
			case installed:
				u.quit()
			case errors.Is(err, context.Canceled): // by the user
			default:
				u.updateFailed(r, err)
			}
		})
	})
}

// fetchAndInstall downloads release r and installs it in place of target,
// calling installing in between. It reports whether r was installed.
func (u *ui) fetchAndInstall(ctx context.Context, r update.Release, target string, progress func(done, total int64), installing func()) (bool, error) {
	dir, err := os.MkdirTemp("", "mugig-download-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	pkg, err := u.updater.download(ctx, r, u.updater.asset, dir, progress)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return false, err
	}
	installing()
	if err := u.updater.install(pkg, target); err != nil {
		return false, err
	}
	return true, nil
}

// updateFailed says why release r couldn't be installed, offering its
// page instead.
func (u *ui) updateFailed(r update.Release, err error) {
	why := err.Error()
	if ue := (*url.Error)(nil); errors.As(err, &ue) {
		why = "the download didn't finish. Check that you're online, then try again"
	}
	msg := widget.NewLabel(fmt.Sprintf("Mugig %s couldn't be installed: %s.\n\nYou can download it from its page instead.", r.Version, why))
	msg.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustomConfirm("Couldn't Update Mugig", "Open Its Page", "Close", msg, func(ok bool) {
		if ok {
			u.openPage(r)
		}
	}, u.win)
	d.Resize(fyne.NewSize(aboutWidth, 0))
	d.Show()
}
