package main

// The window's structure:
//
//	┌──────────┬──────────────────────────────┐
//	│ sidebar  │ page: Songs (header + list)  │
//	│          │    or Now Playing (visualizer)│
//	├──────────┴──────────────────────────────┤
//	│ player bar: track │ controls │ volume   │
//	└─────────────────────────────────────────┘

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const sidebarWidth = 220

// buildSidebar creates the left column: app name, page navigation, and the
// music folder with its scan controls.
func (u *ui) buildSidebar() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(28, 28))
	brand := container.NewHBox(logo,
		widget.NewLabelWithStyle("Mugig", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	u.navSongs = navButton("Songs", theme.ListIcon(), func() { u.showVisualizer(false) })
	u.navNowPlaying = navButton("Now Playing", graphicEqIcon, func() { u.showVisualizer(true) })

	u.folderName = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	u.folderName.Truncation = fyne.TextTruncateEllipsis
	u.folderPath = secondaryLabel("")
	u.folderPath.SizeName = theme.SizeNameCaptionText
	u.showFolder(u.lastFolder())

	u.rescanBtn = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.rescan)
	scanRow := container.NewBorder(nil, nil, nil, u.rescanBtn, u.chooseBtn)

	top := container.NewVBox(
		brand,
		layout.NewSpacer(),
		sectionLabel("LIBRARY"),
		u.navSongs,
		u.navNowPlaying,
	)
	bottom := container.NewVBox(
		sectionLabel("SOURCE"),
		u.folderName,
		u.folderPath,
		scanRow,
		u.cancelBtn,
		u.progress,
		u.status,
	)
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(sidebarWidth, 0))
	return container.NewStack(
		newSurface(colorNameSurface),
		width,
		container.NewPadded(container.NewBorder(top, bottom, nil, nil)),
	)
}

// navButton is a sidebar entry; the current page's entry is filled in.
func navButton(label string, icon fyne.Resource, onTap func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, onTap)
	b.Alignment = widget.ButtonAlignLeading
	b.Importance = widget.LowImportance
	return b
}

// updateNav marks which page is showing.
func (u *ui) updateNav(nowPlaying bool) {
	for b, on := range map[*widget.Button]bool{u.navSongs: !nowPlaying, u.navNowPlaying: nowPlaying} {
		if on {
			b.Importance = widget.MediumImportance
		} else {
			b.Importance = widget.LowImportance
		}
		b.Refresh()
	}
}

// showFolder shows the music folder in the sidebar, with the home folder
// abbreviated to ~.
func (u *ui) showFolder(dir string) {
	if dir == "" {
		u.folderName.SetText("No folder chosen")
		u.folderPath.SetText("Choose where your music is")
		return
	}
	u.folderName.SetText(filepath.Base(dir))
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		dir = "~" + dir[len(home):]
	}
	u.folderPath.SetText(dir)
}

// buildSongsPage creates the Songs page: a header with title, library
// stats and search, above the track list.
func (u *ui) buildSongsPage() fyne.CanvasObject {
	title := widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyleHeading, Text: "Songs"})
	search := container.NewVBox(layout.NewSpacer(),
		container.NewGridWrap(fyne.NewSize(320, u.search.MinSize().Height), u.search),
		layout.NewSpacer())
	checkAll, progress := u.buildBatchBar()
	header := container.NewBorder(nil, nil, nil, container.NewHBox(checkAll, search), container.NewVBox(title, u.count))
	u.songsPage = container.NewBorder(container.NewPadded(container.NewVBox(header, progress)), nil, nil, nil, u.table)
	return u.songsPage
}

// libraryStats summarises the library, e.g. "1,234 tracks · 120 artists ·
// 87 albums".
func libraryStats(tracks []Track) string {
	if len(tracks) == 0 {
		return "No music yet. Choose a folder to scan."
	}
	artists, albums := map[string]bool{}, map[string]bool{}
	for _, t := range tracks {
		if t.Artist != "" {
			artists[strings.ToLower(t.Artist)] = true
		}
		if t.Album != "" {
			albums[strings.ToLower(t.Artist+"\x00"+t.Album)] = true
		}
	}
	return fmt.Sprintf("%s · %s · %s",
		plural(len(tracks), "track"), plural(len(artists), "artist"), plural(len(albums), "album"))
}

func plural(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return thousands(n) + " " + noun
}

// thousands formats n with comma separators.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ---------------------------------------------------------------------------
// Track list cells
// ---------------------------------------------------------------------------

// trackCell is one cell of the track list. The playing track's title gets a
// speaker icon, and its title and artist are drawn in the accent colour.
type trackCell struct {
	widget.BaseWidget
	text   *widget.RichText
	seg    *widget.TextSegment
	icon   *widget.Icon
	onTap  func(at fyne.Position) // passes a click on to the table
	onMenu func(at fyne.Position) // shows the row's right-click menu
}

// TappedSecondary opens the row's menu where the pointer is.
func (c *trackCell) TappedSecondary(e *fyne.PointEvent) {
	if c.onMenu != nil {
		c.onMenu(e.AbsolutePosition)
	}
}

// Tapped passes the click on to the table, which selects the row and so
// plays it. Handling right-clicks makes the cell receive left-clicks too, so
// without this they would go nowhere.
func (c *trackCell) Tapped(e *fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap(e.AbsolutePosition)
	}
}

func newTrackCell(onTap func(at fyne.Position)) fyne.CanvasObject {
	c := &trackCell{
		onTap: onTap,
		seg:   &widget.TextSegment{Style: widget.RichTextStyleInline},
		icon:  widget.NewIcon(theme.NewColoredResource(theme.VolumeUpIcon(), colorNameAccent)),
	}
	c.text = widget.NewRichText(c.seg)
	c.text.Truncation = fyne.TextTruncateEllipsis
	c.icon.Hide()
	c.ExtendBaseWidget(c)
	return c
}

func (c *trackCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewBorder(nil, nil, c.icon, nil, c.text))
}

// secondaryColumns are shown in muted text: year, format and path.
var secondaryColumns = map[int]bool{3: true, 4: true, 6: true}

func (c *trackCell) set(text string, col int, playing bool) {
	c.icon.Hidden = !(playing && col == 0)
	switch {
	case playing && col <= 1:
		c.seg.Style.ColorName = colorNameAccent
	case secondaryColumns[col]:
		c.seg.Style.ColorName = theme.ColorNameDisabled
	default:
		c.seg.Style.ColorName = theme.ColorNameForeground
	}
	c.seg.Text = text
	c.text.Refresh()
	c.Refresh()
}

// setColoured shows text in colour, with no playing marker: the Quality
// column's verdicts.
func (c *trackCell) setColoured(text string, colour fyne.ThemeColorName) {
	c.icon.Hidden = true
	c.seg.Style.ColorName = colour
	c.seg.Text = text
	c.text.Refresh()
	c.Refresh()
}

// ---------------------------------------------------------------------------
// Player bar
// ---------------------------------------------------------------------------

// playerBarLayout lays out three zones (track info, controls, volume) so
// the controls stay in the middle of the window: the side zones get equal
// widths, about 28% each, but never less than they need.
type playerBarLayout struct{}

func (playerBarLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	left, centre, right := objs[0], objs[1], objs[2]
	side := max(size.Width*0.28, left.MinSize().Width, right.MinSize().Width)
	side = max(min(side, (size.Width-centre.MinSize().Width)/2), 0)
	place := func(o fyne.CanvasObject, x, w float32) {
		h := o.MinSize().Height
		o.Resize(fyne.NewSize(w, h))
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
	}
	place(left, 0, side)
	place(centre, side, size.Width-2*side)
	place(right, size.Width-side, side)
}

func (playerBarLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	left, centre, right := objs[0].MinSize(), objs[1].MinSize(), objs[2].MinSize()
	return fyne.NewSize(2*max(left.Width, right.Width)+centre.Width,
		max(left.Height, centre.Height, right.Height))
}

// artTile shows album art in a rounded square, cropped to fill it, or a
// muted note when there is none.
type artTile struct {
	img         *canvas.Image
	placeholder *widget.Icon
	box         *fyne.Container
}

func newArtTile(size float32) *artTile {
	t := &artTile{
		img:         canvas.NewImageFromResource(nil),
		placeholder: widget.NewIcon(theme.NewColoredResource(theme.MediaMusicIcon(), theme.ColorNamePlaceHolder)),
	}
	t.img.FillMode = canvas.ImageFillCover
	t.img.CornerRadius = size / 10
	t.img.Hide()
	pad := size * 0.28
	t.box = container.NewGridWrap(fyne.NewSize(size, size), container.NewStack(
		newRoundedSurface(theme.ColorNameInputBackground, size/10),
		container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), t.placeholder),
		t.img,
	))
	return t
}

// set shows the encoded image data, or the placeholder for nil.
func (t *artTile) set(data []byte) {
	if data == nil {
		t.img.Hide()
		t.placeholder.Show()
		return
	}
	t.img.Resource = fyne.NewStaticResource("cover", data)
	t.img.Show()
	t.placeholder.Hide()
	t.img.Refresh()
}
