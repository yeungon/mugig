package main

// Playlists in the window: the PLAYLISTS section of the sidebar, adding
// and removing songs from their right-click menu, and the dialogs that
// name a playlist and set a smart playlist's rules.

import (
	"fmt"
	"mugig/internal/humanize"
	"mugig/internal/library"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Material Design icons (Apache 2.0): playlist_play and filter_alt.
var (
	playlistIcon      = svgIcon("playlist.svg", "M19 9H2v2h17V9zm0-4H2v2h17V5zM2 15h13v-2H2v2zm15-2v6l5-3-5-3z")
	smartPlaylistIcon = svgIcon("smart_playlist.svg", "M4.25 5.61C6.27 8.2 10 13 10 13v6c0 .55.45 1 1 1h2c.55 0 1-.45 1-1v-6s3.72-4.8 5.74-7.39c.51-.66.04-1.61-.79-1.61H5.04c-.83 0-1.3.95-.79 1.61z")
)

// buildPlaylistSection makes the PLAYLISTS heading, with its new playlist
// button, and the list of playlists below it.
func (u *ui) buildPlaylistSection() (header, list fyne.CanvasObject) {
	u.newPlaylistBtn = iconButton(theme.ContentAddIcon(), func() {
		at := fyne.CurrentApp().Driver().AbsolutePositionForObject(u.newPlaylistBtn)
		widget.ShowPopUpMenuAtPosition(fyne.NewMenu("",
			fyne.NewMenuItem("New Playlist…", func() { u.newPlaylist(nil) }),
			fyne.NewMenuItem("New Smart Playlist…", func() { u.editSmartPlaylist(nil) }),
		), u.win.Canvas(), at.AddXY(0, u.newPlaylistBtn.Size().Height))
	})
	header = container.NewBorder(nil, nil, sectionLabel("PLAYLISTS"), u.newPlaylistBtn)
	u.playlistList = container.NewVBox()
	u.rebuildPlaylistList()
	return header, u.playlistList
}

// rebuildPlaylistList makes a row for each playlist, after one is added,
// renamed or deleted.
func (u *ui) rebuildPlaylistList() {
	if u.playlistList == nil {
		return // not built yet
	}
	u.playlistList.RemoveAll()
	u.playlistRows = make(map[*library.Playlist]*folderRow, len(u.playlists))
	if len(u.playlists) == 0 {
		hint := secondaryLabel("Right-click songs and choose Add to Playlist, or use the +.")
		hint.Wrapping = fyne.TextWrapWord
		hint.SizeName = theme.SizeNameCaptionText
		u.playlistList.Add(hint)
	}
	for _, p := range u.playlists {
		row := newFolderRow(func() { u.showPlaylist(p) }, func(at fyne.Position) {
			widget.ShowPopUpMenuAtPosition(u.playlistMenu(p), u.win.Canvas(), at)
		})
		u.playlistRows[p] = row
		u.playlistList.Add(row)
	}
	u.refreshPlaylists()
}

// refreshPlaylists brings the rows up to date: counts, length, and which
// one is shown.
func (u *ui) refreshPlaylists() {
	if len(u.playlists) == 0 {
		return
	}
	page := u.visView == nil || !u.visView.Visible()
	byPath, facts := library.TracksByPath(u.all), u.songFacts()
	for _, p := range u.playlists {
		row := u.playlistRows[p]
		if row == nil {
			continue
		}
		ts := p.Tracks(u.all, byPath, facts)
		var length time.Duration
		for _, t := range ts {
			length += t.Length
		}
		icon, detail := playlistIcon, "Playlist"
		switch {
		case len(ts) == 0:
			detail = "Empty"
		case length > 0:
			detail = humanize.TotalTime(length)
		}
		if p.Smart != nil {
			icon, detail = smartPlaylistIcon, "Smart · "+detail
			if len(ts) > 0 && length == 0 {
				detail = "Smart playlist"
			}
		}
		row.show(icon, p.Name, detail, humanize.Thousands(len(ts)), true, page && u.playlist == p)
	}
}

// songFacts is what smart playlists' rules need beyond a song's tags.
func (u *ui) songFacts() library.SongFacts {
	return library.SongFacts{Level: u.levelOf, ModTime: u.cache.ModTime, Now: time.Now()}
}

// showPlaylist shows p's songs in the song list.
func (u *ui) showPlaylist(p *library.Playlist) {
	u.filter, u.playlist = "", p
	u.table.UnselectAll()
	u.table.ScrollToTop()
	u.applyView()
	u.showVisualizer(false) // also marks the playlist in the sidebar
}

// playlistsChanged saves the playlists and shows the change.
func (u *ui) playlistsChanged() {
	if err := library.SavePlaylists(u.playlistsPath, u.playlists); err != nil {
		fyne.LogError("saving playlists", err)
	}
	if u.playlist != nil {
		u.applyView()
	} else {
		u.updateCount()
	}
	u.refreshPlaylists()
}

// playlistMenu is a playlist's right-click menu in the sidebar.
func (u *ui) playlistMenu(p *library.Playlist) *fyne.Menu {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Play", func() { u.playPlaylist(p) }),
		fyne.NewMenuItemSeparator(),
	}
	if p.Smart != nil {
		items = append(items, fyne.NewMenuItem("Edit Rules…", func() { u.editSmartPlaylist(p) }))
	}
	items = append(items,
		fyne.NewMenuItem("Rename…", func() { u.renamePlaylist(p) }),
		fyne.NewMenuItem("Duplicate", func() { u.duplicatePlaylist(p) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Delete Playlist…", func() { u.confirmDeletePlaylist(p) }),
	)
	return fyne.NewMenu("", items...)
}

// playPlaylist shows p and plays it from the top.
func (u *ui) playPlaylist(p *library.Playlist) {
	u.showPlaylist(p)
	if len(u.tracks) > 0 {
		u.jump(u.tracks[0])
	}
}

// askName asks for a playlist's name, offering name, and calls done with
// what was typed.
func (u *ui) askName(title, confirm, name string, done func(string)) {
	entry := widget.NewEntry()
	entry.SetText(name)
	entry.Validator = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("a playlist needs a name")
		}
		return nil
	}
	d := dialog.NewForm(title, confirm, "Cancel", []*widget.FormItem{widget.NewFormItem("Name", entry)}, func(ok bool) {
		if ok {
			done(strings.TrimSpace(entry.Text))
		}
	}, u.win)
	d.Resize(fyne.NewSize(380, d.MinSize().Height))
	d.Show()
	u.win.Canvas().Focus(entry)
}

// newPlaylist asks for a name and makes a playlist of songs, which may be
// none. Made from the + button, it is then shown; made from songs, the
// list stays where it is.
func (u *ui) newPlaylist(songs []library.Track) {
	u.askName("New Playlist", "Create", library.UniqueName(u.playlists, "New Playlist"), func(name string) {
		p := &library.Playlist{Name: library.UniqueName(u.playlists, name)}
		for _, t := range songs {
			p.Add(t.Path)
		}
		u.playlists = append(u.playlists, p)
		u.rebuildPlaylistList()
		u.playlistsChanged()
		if songs == nil {
			u.showPlaylist(p)
		} else {
			u.status.SetText(fmt.Sprintf("Added %s to %s", humanize.Plural(len(songs), "song"), p.Name))
		}
	})
}

// addToPlaylist adds songs to the end of p, leaving out any already in it.
func (u *ui) addToPlaylist(p *library.Playlist, songs []library.Track) {
	paths := make([]string, len(songs))
	for i, t := range songs {
		paths[i] = t.Path
	}
	added := p.Add(paths...)
	u.playlistsChanged()
	msg := fmt.Sprintf("Added %s to %s", humanize.Plural(added, "song"), p.Name)
	if added < len(songs) {
		msg += fmt.Sprintf(" (%d already in it)", len(songs)-added)
	}
	u.status.SetText(msg)
}

// addToPlaylistItem is the songs' "Add to Playlist" submenu: the ordinary
// playlists, and a new one.
func (u *ui) addToPlaylistItem(songs []library.Track) *fyne.MenuItem {
	var items []*fyne.MenuItem
	for _, p := range u.playlists {
		if p.Smart == nil {
			items = append(items, fyne.NewMenuItem(p.Name, func() { u.addToPlaylist(p, songs) }))
		}
	}
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	items = append(items, fyne.NewMenuItem("New Playlist…", func() { u.newPlaylist(songs) }))
	sub := fyne.NewMenuItem("Add to Playlist", nil)
	sub.ChildMenu = fyne.NewMenu("", items...)
	return sub
}

// playlistSongItems are the extra right-click items for songs shown in an
// ordinary playlist: taking them out, and for one song, moving it, while
// the list is in the playlist's own order.
func (u *ui) playlistSongItems(songs []library.Track) []*fyne.MenuItem {
	p := u.playlist
	if p == nil || p.Smart != nil {
		return nil
	}
	remove := fyne.NewMenuItem("Remove from Playlist", func() {
		for _, t := range songs {
			p.Remove(t.Path)
		}
		u.playlistsChanged()
	})
	items := []*fyne.MenuItem{remove}
	if len(songs) == 1 && u.sortCol < 0 && len(u.searchWords()) == 0 {
		i := u.indexOf(songs[0].Path)
		move := func(by int) func() {
			return func() {
				p.Move(songs[0].Path, by, u.tracks)
				u.playlistsChanged()
			}
		}
		up, down := fyne.NewMenuItem("Move Up", move(-1)), fyne.NewMenuItem("Move Down", move(1))
		up.Disabled, down.Disabled = i <= 0, i < 0 || i >= len(u.tracks)-1
		items = append(items, up, down)
	}
	return items
}

func (u *ui) renamePlaylist(p *library.Playlist) {
	u.askName("Rename Playlist", "Rename", p.Name, func(name string) {
		if !strings.EqualFold(name, p.Name) {
			name = library.UniqueName(u.playlists, name)
		}
		p.Name = name
		u.playlistsChanged()
	})
}

func (u *ui) duplicatePlaylist(p *library.Playlist) {
	c := &library.Playlist{Name: library.UniqueName(u.playlists, p.Name+" copy"), Songs: slices.Clone(p.Songs)}
	if p.Smart != nil {
		c.Smart = &library.SmartRules{Any: p.Smart.Any, Rules: slices.Clone(p.Smart.Rules)}
	}
	i := slices.Index(u.playlists, p)
	u.playlists = slices.Insert(u.playlists, i+1, c)
	u.rebuildPlaylistList()
	u.playlistsChanged()
}

func (u *ui) confirmDeletePlaylist(p *library.Playlist) {
	msg := widget.NewLabel("The songs stay in the library.")
	d := dialog.NewCustomConfirm(fmt.Sprintf("Delete “%s”?", p.Name), "Delete", "Cancel", msg, func(ok bool) {
		if ok {
			u.deletePlaylist(p)
		}
	}, u.win)
	d.SetConfirmImportance(widget.DangerImportance)
	d.Show()
}

func (u *ui) deletePlaylist(p *library.Playlist) {
	u.playlists = slices.DeleteFunc(u.playlists, func(q *library.Playlist) bool { return q == p })
	if u.playlist == p {
		u.setFilter("")
	}
	u.rebuildPlaylistList()
	u.playlistsChanged()
}

// forgetInPlaylists takes songs whose files are gone out of the ordinary
// playlists.
func (u *ui) forgetInPlaylists(paths []string) {
	changed := false
	for _, p := range u.playlists {
		n := len(p.Songs)
		p.Remove(paths...)
		changed = changed || len(p.Songs) != n
	}
	if changed {
		u.playlistsChanged()
	}
}

// saveQueueAsPlaylist makes a playlist of the songs in Up Next.
func (u *ui) saveQueueAsPlaylist() {
	if len(u.queued) > 0 {
		u.newPlaylist(slices.Clone(u.queued))
	}
}

// editSmartPlaylist opens the rules of p, or of a new smart playlist for
// nil, in a dialog that counts the songs matching as the rules change.
func (u *ui) editSmartPlaylist(p *library.Playlist) {
	rules := library.SmartRules{Rules: []library.SmartRule{{Field: "Artist", Op: library.OpContains}}}
	name := library.UniqueName(u.playlists, "New Smart Playlist")
	title, confirm := "New Smart Playlist", "Create"
	if p != nil {
		rules = library.SmartRules{Any: p.Smart.Any, Rules: slices.Clone(p.Smart.Rules)}
		name, title, confirm = p.Name, "Edit Smart Playlist", "Save"
	}
	e := u.newRulesEditor(&rules)
	nameEntry := widget.NewEntry()
	nameEntry.SetText(name)
	content := container.NewVBox(
		widget.NewForm(widget.NewFormItem("Name", nameEntry)),
		e.box,
	)
	d := dialog.NewCustomConfirm(title, confirm, "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			name = "Smart Playlist"
		}
		if p == nil {
			p = &library.Playlist{Name: library.UniqueName(u.playlists, name), Smart: &rules}
			u.playlists = append(u.playlists, p)
			u.rebuildPlaylistList()
			u.playlistsChanged()
			u.showPlaylist(p)
			return
		}
		if !strings.EqualFold(name, p.Name) {
			name = library.UniqueName(u.playlists, name)
		}
		p.Name, p.Smart = name, &rules
		u.playlistsChanged()
	}, u.win)
	e.onResize = func() { d.Resize(fyne.NewSize(620, d.MinSize().Height)) }
	e.onResize()
	d.Show()
	u.rulesDialog = e
}

// rulesEditor is the smart playlist dialog's rules: a row per rule, and
// how many songs they match.
type rulesEditor struct {
	u        *ui
	rules    *library.SmartRules
	box      *fyne.Container
	rows     *fyne.Container
	any      *widget.Select
	count    *widget.Label
	onResize func()
}

func (u *ui) newRulesEditor(rules *library.SmartRules) *rulesEditor {
	e := &rulesEditor{u: u, rules: rules, rows: container.NewVBox(), count: secondaryLabel("")}
	e.count.Truncation = fyne.TextTruncateOff
	e.any = widget.NewSelect([]string{"all", "any"}, func(s string) {
		e.rules.Any = s == "any"
		e.update()
	})
	if rules.Any {
		e.any.SetSelected("any")
	} else {
		e.any.SetSelected("all")
	}
	add := widget.NewButtonWithIcon("Add Rule", theme.ContentAddIcon(), func() {
		e.rules.Rules = append(e.rules.Rules, library.SmartRule{Field: "Artist", Op: library.OpContains})
		e.build()
	})
	add.Importance = widget.LowImportance
	e.box = container.NewVBox(
		container.NewHBox(widget.NewLabel("Songs matching"), e.any, widget.NewLabel("of these rules:")),
		e.rows,
		container.NewHBox(add, layout.NewSpacer(), e.count),
	)
	e.build()
	return e
}

// build makes the rows from the rules.
func (e *rulesEditor) build() {
	e.rows.RemoveAll()
	for i := range e.rules.Rules {
		e.rows.Add(e.ruleRow(i))
	}
	e.update()
	if e.onResize != nil {
		e.onResize()
	}
}

// ruleRow is rule i: its field, comparison and value, and a button to
// take it away.
func (e *rulesEditor) ruleRow(i int) fyne.CanvasObject {
	r := &e.rules.Rules[i]
	kind, _ := library.FieldKindOf(r.Field)
	var names []string
	for _, f := range library.SmartFields {
		names = append(names, f.Name)
	}
	field := widget.NewSelect(names, nil)
	field.SetSelected(r.Field)
	field.OnChanged = func(name string) {
		newKind, _ := library.FieldKindOf(name)
		r.Field = name
		if newKind != kind { // the comparisons and value no longer fit
			r.Op, r.Value = library.KindOps[newKind][0], ""
			if newKind == library.QualityField {
				r.Value = library.QualityChoices()[0]
			}
		}
		e.build()
	}
	op := widget.NewSelect(library.KindOps[kind], func(s string) { r.Op = s; e.update() })
	op.SetSelected(r.Op)
	var value fyne.CanvasObject
	if kind == library.QualityField {
		v := widget.NewSelect(library.QualityChoices(), func(s string) { r.Value = s; e.update() })
		v.SetSelected(r.Value)
		value = v
	} else {
		v := widget.NewEntry()
		v.SetText(r.Value)
		v.OnChanged = func(s string) { r.Value = s; e.update() }
		value = v
	}
	if unit := library.KindUnits[kind]; unit != "" {
		value = container.NewBorder(nil, nil, nil, secondaryLabel(unit), value)
	}
	remove := iconButton(theme.ContentRemoveIcon(), func() {
		e.rules.Rules = slices.Delete(e.rules.Rules, i, i+1)
		e.build()
	})
	if len(e.rules.Rules) == 1 {
		remove.Disable() // a smart playlist has at least one rule
	}
	return container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(300, field.MinSize().Height), container.NewGridWithColumns(2, field, op)),
		remove, value)
}

// update counts the songs the rules match.
func (e *rulesEditor) update() {
	n := 0
	facts := e.u.songFacts()
	for _, t := range e.u.all {
		if e.rules.Matches(t, facts) {
			n++
		}
	}
	verb := "match"
	if n == 1 {
		verb = "matches"
	}
	e.count.SetText(fmt.Sprintf("%s %s", humanize.Plural(n, "song"), verb))
}
