package main

// Up Next: songs chosen with Play Next or Play Later from a song's
// right-click menu. They play before the list carries on, and the list then
// carries on from the last song played from it, as if they had been slipped
// in. The player bar's queue button lights up while any are waiting, and
// shows them.

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// queueIcon is Material Design's "queue_music" (Apache 2.0).
var queueIcon = svgIcon("queue_music.svg", "M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18c-.31-.11-.65-.18-1-.18-1.66 0-3 1.34-3 3s1.34 3 3 3 3-1.34 3-3V8h3V6h-5z")

// playNext puts songs at the front of Up Next, in the order given.
func (u *ui) playNext(ts ...Track) {
	u.queued = slices.Insert(u.queued, 0, ts...)
	u.queueChanged()
}

// playLater puts songs at the end of Up Next.
func (u *ui) playLater(ts ...Track) {
	u.queued = append(u.queued, ts...)
	u.queueChanged()
}

// clearQueue empties Up Next.
func (u *ui) clearQueue() {
	u.queued = nil
	u.queueChanged()
}

// takeQueued removes the first song from Up Next and returns it, skipping
// songs no longer in the library (their folder was removed, or the file
// deleted).
func (u *ui) takeQueued() (Track, bool) {
	defer u.queueChanged()
	for len(u.queued) > 0 {
		t := u.queued[0]
		u.queued = u.queued[1:]
		if t, ok := u.inLibrary(t.Path); ok {
			return t, true
		}
	}
	return Track{}, false
}

// pruneQueue drops songs no longer in the library from Up Next.
func (u *ui) pruneQueue() {
	u.queued = slices.DeleteFunc(u.queued, func(t Track) bool {
		_, ok := u.inLibrary(t.Path)
		return !ok
	})
}

// inLibrary returns the library's copy of the song at path, which has its
// latest tags.
func (u *ui) inLibrary(path string) (Track, bool) {
	for _, t := range u.all {
		if t.Path == path {
			return t, true
		}
	}
	return Track{}, false
}

// playQueuedNow plays the i'th song of Up Next straight away, taking it
// out of Up Next.
func (u *ui) playQueuedNow(i int) {
	if i < 0 || i >= len(u.queued) {
		return
	}
	t := u.queued[i]
	u.queued = slices.Delete(u.queued, i, i+1)
	u.queueChanged()
	if t, ok := u.inLibrary(t.Path); ok {
		u.remember()
		u.start(t, true)
	}
}

// removeQueued takes the i'th song out of Up Next.
func (u *ui) removeQueued(i int) {
	if i >= 0 && i < len(u.queued) {
		u.queued = slices.Delete(u.queued, i, i+1)
		u.queueChanged()
	}
}

// queueChanged shows whether songs are waiting: on the queue button, in
// the menu, and in the Up Next list if it is open.
func (u *ui) queueChanged() {
	u.saveQueue()
	if u.queueBtn != nil {
		setToggle(u.queueBtn, queueIcon, len(u.queued) > 0)
	}
	if u.clearQueueItem != nil && u.clearQueueItem.Disabled != (len(u.queued) == 0) {
		u.clearQueueItem.Disabled = len(u.queued) == 0
		u.mainMenu.Refresh()
	}
	if u.queuePop != nil && u.queuePop.Visible() {
		u.fillQueuePopUp()
	}
}

// showQueue opens the Up Next list above the queue button.
func (u *ui) showQueue() {
	c := fyne.CurrentApp().Driver().CanvasForObject(u.queueBtn)
	if c == nil {
		return
	}
	u.pruneQueue()
	if u.queuePop == nil {
		u.queuePop = widget.NewPopUp(container.NewStack(), c)
	}
	u.fillQueuePopUp()
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(u.queueBtn)
	size := u.queuePop.MinSize()
	// Above the button, as the player bar is at the bottom of the window,
	// and kept inside the window on the right.
	x := min(at.X, c.Size().Width-size.Width-theme.Padding())
	u.queuePop.ShowAtPosition(fyne.NewPos(max(x, 0), max(at.Y-size.Height, 0)))
}

// queueWidth and queueRows size the Up Next list: rows beyond queueRows
// scroll.
const (
	queueWidth = 340
	queueRows  = 8
)

// fillQueuePopUp builds the Up Next list's content from u.queued.
func (u *ui) fillQueuePopUp() {
	title := widget.NewLabelWithStyle("Up Next", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	clear := widget.NewButton("Clear", u.clearQueue)
	clear.Importance = widget.LowImportance
	save := widget.NewButton("Save as Playlist…", func() {
		u.queuePop.Hide()
		u.saveQueueAsPlaylist()
	})
	save.Importance = widget.LowImportance
	var body fyne.CanvasObject
	if len(u.queued) == 0 {
		clear.Disable()
		save.Disable()
		hint := secondaryLabel("Nothing queued. Right-click a song and choose Play Next or Play Later.")
		hint.Wrapping = fyne.TextWrapWord
		body = hint
	} else {
		list := widget.NewList(
			func() int { return len(u.queued) },
			func() fyne.CanvasObject { return newQueueRow() },
			func(i widget.ListItemID, o fyne.CanvasObject) {
				if i < len(u.queued) {
					o.(*queueRow).set(u.queued[i], func() { u.removeQueued(i) })
				}
			},
		)
		list.OnSelected = func(i widget.ListItemID) {
			list.UnselectAll()
			u.queuePop.Hide()
			u.playQueuedNow(i)
		}
		// Tall enough for the songs, up to queueRows: the list puts a
		// padding's height between rows.
		rows := float32(min(len(u.queued), queueRows))
		height := rows*newQueueRow().MinSize().Height + (rows-1)*theme.Padding()
		body = container.NewGridWrap(fyne.NewSize(queueWidth, height), list)
	}
	content := container.NewBorder(container.NewBorder(nil, nil, title, container.NewHBox(save, clear)), nil, nil, nil, body)
	u.queuePop.Content = container.NewGridWrap(fyne.NewSize(queueWidth, content.MinSize().Height), content)
	u.queuePop.Refresh()
	u.queuePop.Resize(u.queuePop.MinSize())
}

// queueRow is one song in the Up Next list: its title, with the artist and
// length under it, and a button to take it out.
type queueRow struct {
	widget.BaseWidget
	title, detail *widget.Label
	remove        *widget.Button
}

func newQueueRow() *queueRow {
	r := &queueRow{title: widget.NewLabel(""), detail: secondaryLabel("")}
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.detail.Truncation = fyne.TextTruncateEllipsis
	r.detail.SizeName = theme.SizeNameCaptionText
	r.remove = iconButton(theme.ContentClearIcon(), nil)
	r.ExtendBaseWidget(r)
	return r
}

func (r *queueRow) CreateRenderer() fyne.WidgetRenderer {
	text := container.New(&tightRows{}, r.title, r.detail)
	return widget.NewSimpleRenderer(container.NewBorder(nil, nil, nil, container.NewCenter(r.remove), text))
}

func (r *queueRow) set(t Track, remove func()) {
	r.title.SetText(t.Title)
	var detail []string
	for _, s := range []string{t.Artist, cellText(t, timeCol)} {
		if s != "" {
			detail = append(detail, s)
		}
	}
	r.detail.SetText(strings.Join(detail, " · "))
	r.remove.OnTapped = remove
}

// tightRows stacks objects with the label padding between them removed,
// so a title and its caption read as one item.
type tightRows struct{}

func (*tightRows) overlap() float32 { return 2 * theme.Padding() }

func (l *tightRows) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objs {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h - l.overlap()
	}
}

func (l *tightRows) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objs {
		m := o.MinSize()
		s.Width = max(s.Width, m.Width)
		s.Height += m.Height - l.overlap()
	}
	return s.AddWidthHeight(0, l.overlap())
}
