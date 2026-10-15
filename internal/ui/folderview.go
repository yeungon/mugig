package ui

// The sidebar's list of music folders: each shows its name, where it is
// and how many songs it holds. Clicking one narrows the song list to it;
// right-clicking offers to rescan it, show it in Finder, or remove it.

import (
	"mugig/internal/humanize"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// buildFolderSection makes the FOLDERS heading, with its rescan and add
// buttons, and the list of folders below it.
func (u *ui) buildFolderSection() (header, list fyne.CanvasObject) {
	u.rescanBtn = iconButton(theme.ViewRefreshIcon(), u.scanLibrary)
	u.addBtn = iconButton(theme.ContentAddIcon(), u.chooseFolderToAdd)
	header = container.NewBorder(nil, nil, sectionLabel("FOLDERS"), container.NewHBox(u.rescanBtn, u.addBtn))
	u.folderList = container.NewVBox()
	u.rebuildFolderList()
	return header, u.folderList
}

// rebuildFolderList makes a row for each folder, after one is added or
// removed.
func (u *ui) rebuildFolderList() {
	if u.folderList == nil {
		return // not built yet
	}
	u.folderList.RemoveAll()
	if len(u.folders) == 0 {
		add := widget.NewButtonWithIcon("Add Music Folder…", theme.FolderOpenIcon(), u.chooseFolderToAdd)
		add.Importance = widget.HighImportance
		u.folderList.Add(add)
	}
	for _, f := range u.folders {
		f.row = newFolderRow(func() { u.setFilter(f.path) }, func(at fyne.Position) {
			widget.ShowPopUpMenuAtPosition(u.folderMenu(f), u.win.Canvas(), at)
		})
		u.folderList.Add(f.row)
	}
	u.refreshFolders()
	u.updateCount()
}

// refreshFolders brings the rows up to date: counts, connection, and which
// one is chosen.
func (u *ui) refreshFolders() {
	counts := make(map[*musicFolder]int, len(u.folders))
	for _, t := range u.all {
		if f := u.containing(t.Path); f != nil {
			counts[f]++
		}
	}
	defer u.refreshPlaylists()
	page := u.visView == nil || !u.visView.Visible() // the song list is showing
	for _, f := range u.folders {
		if f.row == nil {
			continue
		}
		detail := folderPlace(f.path)
		switch {
		case !f.connected:
			detail = f.missing
		case u.scanning == f.path:
			detail = "Scanning…"
		}
		f.row.set(f, counts[f], detail, page && u.playlist == nil && u.filter == f.path)
	}
}

// folderMenu is a folder's right-click menu.
func (u *ui) folderMenu(f *musicFolder) *fyne.Menu {
	rescan := fyne.NewMenuItem("Rescan", func() { u.queueScan(f.path) })
	show := fyne.NewMenuItem(revealLabel, func() {
		u.async(func() {
			if err := u.reveal(f.path); err != nil {
				onUI(func() { dialog.ShowError(err, u.win) })
			}
		})
	})
	rescan.Disabled, show.Disabled = !f.connected, !f.connected
	remove := fyne.NewMenuItem("Remove from Library…", func() { u.confirmRemoveFolder(f.path) })
	return fyne.NewMenu("", rescan, show, fyne.NewMenuItemSeparator(), remove)
}

// folderPlace says where a folder is, for the line under its name: the
// folder it is in, with the home folder as ~, or on macOS the disk's
// name rather than /Volumes.
func folderPlace(path string) string {
	parent := filepath.Dir(path)
	switch {
	case mountParents[parent] || parent == path:
		return "Disk"
	case runtime.GOOS == "darwin" && strings.HasPrefix(parent, "/Volumes/"):
		return strings.TrimPrefix(parent, "/Volumes/")
	}
	return displayPath(parent)
}

// folderRow is one folder in the sidebar: an icon, the name with where it
// is underneath, and the number of songs.
type folderRow struct {
	widget.BaseWidget
	bg      *surface
	icon    *widget.Icon
	name    *widget.TextSegment
	detail  *widget.TextSegment
	text    *widget.RichText
	count   *widget.Label
	content *fyne.Container
	chosen  bool
	hovered bool
	onTap   func()
	onMenu  func(at fyne.Position)
}

var _ desktop.Hoverable = (*folderRow)(nil)

func newFolderRow(onTap func(), onMenu func(at fyne.Position)) *folderRow {
	r := &folderRow{
		bg:     newRoundedSurface(theme.ColorNameButton, 6),
		icon:   widget.NewIcon(nil),
		name:   &widget.TextSegment{Style: widget.RichTextStyleInline},
		detail: &widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText}},
		count:  secondaryLabel(""),
		onTap:  onTap,
		onMenu: onMenu,
	}
	r.name.Style.Inline = false
	r.text = widget.NewRichText(r.name, r.detail)
	r.text.Truncation = fyne.TextTruncateEllipsis
	r.count.SizeName = theme.SizeNameCaptionText
	r.count.Truncation = fyne.TextTruncateOff
	r.bg.Hide()
	r.content = container.NewStack(r.bg,
		container.New(&rowPadding{theme.Padding()}, container.NewBorder(nil, nil, r.icon, r.count, r.text)))
	r.ExtendBaseWidget(r)
	return r
}

func (r *folderRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

// set shows a folder's state.
func (r *folderRow) set(f *musicFolder, count int, detail string, chosen bool) {
	icon := theme.FolderIcon()
	if onRemovableDisk(f.path) {
		icon = theme.StorageIcon()
	}
	countText := humanize.Thousands(count)
	if !f.connected {
		icon, countText = theme.NewDisabledResource(icon), ""
	}
	r.show(icon, filepath.Base(f.path), detail, countText, f.connected, chosen)
}

// show fills in the row: also used for playlists.
func (r *folderRow) show(icon fyne.Resource, name, detail, countText string, enabled, chosen bool) {
	nameColour := theme.ColorNameForeground
	if !enabled {
		nameColour = theme.ColorNameDisabled
	}
	r.icon.SetResource(icon)
	r.name.Text, r.name.Style.ColorName = name, nameColour
	r.detail.Text = detail
	r.text.Refresh()
	if r.count.Text != countText {
		r.count.SetText(countText)
		r.content.Refresh() // lay out again: the name makes way for a wider count
	}
	r.chosen = chosen
	r.showBackground()
}

func (r *folderRow) showBackground() {
	switch {
	case r.chosen:
		r.bg.color = theme.ColorNameButton
	case r.hovered:
		r.bg.color = theme.ColorNameHover
	default:
		r.bg.Hide()
		return
	}
	r.bg.Show()
	r.bg.Refresh()
}

func (r *folderRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *folderRow) TappedSecondary(e *fyne.PointEvent) {
	if r.onMenu != nil {
		r.onMenu(e.AbsolutePosition)
	}
}

func (r *folderRow) MouseIn(*desktop.MouseEvent)    { r.hovered = true; r.showBackground() }
func (r *folderRow) MouseMoved(*desktop.MouseEvent) {}
func (r *folderRow) MouseOut()                      { r.hovered = false; r.showBackground() }

// rowPadding insets a row's content a little at the sides only, as the
// text already has room above and below.
type rowPadding struct{ side float32 }

func (p *rowPadding) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(p.side, 0))
		o.Resize(fyne.NewSize(size.Width-2*p.side, size.Height))
	}
}

func (p *rowPadding) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objs {
		s = s.Max(o.MinSize())
	}
	return s.Add(fyne.NewSize(2*p.side, 0))
}
