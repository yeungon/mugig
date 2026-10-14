package main

import (
	"bytes"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-text/typesetting/font"
)

// contrast is the WCAG contrast ratio between two opaque colours.
func contrast(a, b color.Color) float64 {
	lum := func(c color.Color) float64 {
		n := toNRGBA(c)
		ch := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(n.R) + 0.7152*ch(n.G) + 0.0722*ch(n.B)
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// Text must meet WCAG AA (4.5:1); icons and fills 3:1.
func TestPaletteContrast(t *testing.T) {
	for name, p := range map[string]map[fyne.ThemeColorName]color.Color{"dark": darkPalette, "light": lightPalette} {
		bg, surf := p[theme.ColorNameBackground], p[colorNameSurface]
		for _, c := range []struct {
			what   string
			fg, bg color.Color
			min    float64
		}{
			{"text", p[theme.ColorNameForeground], bg, 7},
			{"secondary text", p[theme.ColorNameDisabled], bg, 4.5},
			{"secondary text on panels", p[theme.ColorNameDisabled], surf, 4.5},
			{"accent text", p[colorNameAccent], bg, 4.5},
			{"text on primary", p[theme.ColorNameForegroundOnPrimary], p[theme.ColorNamePrimary], 4.5},
			{"primary fill", p[theme.ColorNamePrimary], bg, 3},
			{"placeholder", p[theme.ColorNamePlaceHolder], p[theme.ColorNameInputBackground], 3},
			// Quality badges sit on the player bar and the page.
			{"accent badge on panels", p[colorNameAccent], surf, 4.5},
			{"warning badge", p[theme.ColorNameWarning], bg, 4.5},
			{"warning badge on panels", p[theme.ColorNameWarning], surf, 4.5},
			{"error badge", p[theme.ColorNameError], bg, 4.5},
			{"error badge on panels", p[theme.ColorNameError], surf, 4.5},
			{"success icon", p[theme.ColorNameSuccess], bg, 3},
		} {
			if got := contrast(c.fg, c.bg); got < c.min {
				t.Errorf("%s %s: contrast %.2f, want ≥ %.1f", name, c.what, got, c.min)
			}
		}
	}
}

func TestPalettesMatch(t *testing.T) {
	for n := range darkPalette {
		if _, ok := lightPalette[n]; !ok {
			t.Errorf("light palette lacks %q", n)
		}
	}
	for n := range lightPalette {
		if _, ok := darkPalette[n]; !ok {
			t.Errorf("dark palette lacks %q", n)
		}
	}
	th := appTheme{}
	if th.Color(theme.ColorNameBackground, theme.VariantLight) == th.Color(theme.ColorNameBackground, theme.VariantDark) {
		t.Error("variants should differ")
	}
	if th.Color(theme.ColorNameError, theme.VariantDark) == nil || th.Size(theme.SizeNamePadding) == 0 {
		t.Error("unset names should fall back to the default theme")
	}
}

// variantTheme pins appTheme to one variant, so tests can render both.
type variantTheme struct {
	appTheme
	v fyne.ThemeVariant
}

func (t variantTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.appTheme.Color(n, t.v)
}

func TestWindowRendersInBothVariants(t *testing.T) {
	for _, v := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		u, _, a := newTestUI(t)
		a.Settings().SetTheme(variantTheme{v: v})
		setLibrary(u, fiveTracks()...)
		u.togglePlay()
		u.win.Resize(fyne.NewSize(1000, 600))
		img := u.win.Canvas().Capture()

		want := toNRGBA(appTheme{}.Color(colorNameSurface, v))
		if got := toNRGBA(img.At(4, img.Bounds().Dy()/3)); got != want {
			t.Errorf("variant %v: sidebar pixel %v, want surface %v", v, got, want)
		}
	}
}

func TestSidebarNavigation(t *testing.T) {
	u, _, _ := newTestUI(t)
	if !u.songsPage.Visible() || u.navSongs.Importance != widget.MediumImportance {
		t.Fatal("Songs should be the starting page")
	}
	test.Tap(u.navNowPlaying)
	if u.songsPage.Visible() || !u.visView.Visible() || u.navNowPlaying.Importance != widget.MediumImportance ||
		u.navSongs.Importance != widget.LowImportance {
		t.Error("Now Playing entry should switch pages and be marked")
	}
	test.Tap(u.navSongs)
	if !u.songsPage.Visible() || u.visView.Visible() {
		t.Error("Songs entry should switch back")
	}
}

// Under a folder's name, the sidebar says where it is.
func TestFolderPlace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder")
	}
	want := map[string]string{
		filepath.Join(home, "Music", "Jazz"): filepath.Join("~", "Music"),
		"/Volumes/OneTouch":                  "Disk",
		"/Volumes/OneTouch/2.Music B/Nhạc":   "OneTouch/2.Music B",
		"/Users/shared/Music":                "/Users/shared",
	}
	if runtime.GOOS != "darwin" {
		want["/Volumes/OneTouch/2.Music B/Nhạc"] = "/Volumes/OneTouch/2.Music B"
	}
	for path, place := range want {
		if got := folderPlace(path); got != place {
			t.Errorf("%s: %q, want %q", path, got, place)
		}
	}
}

func TestTrackCellStyles(t *testing.T) {
	newTestApp(t)
	c := newTrackCell(nil).(*trackCell)
	for _, tc := range []struct {
		col     int
		playing bool
		color   fyne.ThemeColorName
		icon    bool
	}{
		{0, false, theme.ColorNameForeground, false},
		{0, true, colorNameAccent, true},
		{1, true, colorNameAccent, false},
		{2, true, theme.ColorNameForeground, false},
		{3, false, theme.ColorNameDisabled, false},
		{6, true, theme.ColorNameDisabled, false}, // path
	} {
		c.set("x", tc.col, tc.playing)
		if c.seg.Style.ColorName != tc.color || c.icon.Visible() != tc.icon {
			t.Errorf("col %d playing=%v: colour %q icon %v", tc.col, tc.playing, c.seg.Style.ColorName, c.icon.Visible())
		}
	}
	c.setColoured("Lossless?", theme.ColorNameWarning)
	if c.seg.Style.ColorName != theme.ColorNameWarning || c.icon.Visible() || c.seg.Text != "Lossless?" {
		t.Errorf("quality cell: %q %q", c.seg.Text, c.seg.Style.ColorName)
	}
}

func TestPlayingRowIsMarked(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.win.Resize(fyne.NewSize(1000, 600))
	u.table.Select(widget.TableCellID{Row: 2, Col: 1})
	if u.playing.Title != "Hey Jude" {
		t.Fatalf("clicking a row played %q", u.playing.Title)
	}
	// The row is marked by colour, not by a single highlighted cell.
	var marked []string
	visit(u.table, func(o fyne.CanvasObject) {
		if c, ok := o.(*trackCell); ok && c.Visible() && c.seg.Style.ColorName == colorNameAccent {
			marked = append(marked, c.seg.Text)
		}
	})
	if len(marked) != 2 || marked[0] == marked[1] { // title and artist
		t.Errorf("accent cells %v, want the title and artist", marked)
	}
}

// visit calls f for o and everything drawn inside it, including the
// insides of widgets.
func visit(o fyne.CanvasObject, f func(fyne.CanvasObject)) {
	f(o)
	switch x := o.(type) {
	case *fyne.Container:
		for _, c := range x.Objects {
			visit(c, f)
		}
	case fyne.Widget:
		for _, c := range test.WidgetRenderer(x).Objects() {
			visit(c, f)
		}
	}
}

func TestPlayerBarKeepsControlsCentred(t *testing.T) {
	sized := func(w float32) fyne.CanvasObject {
		r := canvas.NewRectangle(color.Black)
		r.SetMinSize(fyne.NewSize(w, 40))
		return r
	}
	left, centre, right := sized(150), sized(300), sized(400)
	l := playerBarLayout{}
	l.Layout([]fyne.CanvasObject{left, centre, right}, fyne.NewSize(1600, 60))
	if mid := centre.Position().X + centre.Size().Width/2; mid != 800 {
		t.Errorf("controls centred at %v, want 800", mid)
	}
	if right.Position().X+right.Size().Width != 1600 || right.Size().Width < 400 {
		t.Errorf("right zone at %v width %v", right.Position().X, right.Size().Width)
	}
	if min := l.MinSize([]fyne.CanvasObject{left, centre, right}); min.Width != 2*400+300 {
		t.Errorf("min width %v", min.Width)
	}
}

func TestRoundButtonAndArtTile(t *testing.T) {
	newTestApp(t)
	taps := 0
	b := newRoundButton(theme.MediaPlayIcon(), 40, func() { taps++ })
	test.Tap(b)
	b.SetIcon(theme.MediaPauseIcon())
	if taps != 1 || b.icon != theme.MediaPauseIcon() || b.MinSize() != fyne.NewSize(40, 40) {
		t.Errorf("round button: taps %d, icon %v, size %v", taps, b.icon, b.MinSize())
	}

	a := newArtTile(56)
	if a.img.Visible() || !a.placeholder.Visible() {
		t.Error("empty tile should show the placeholder")
	}
	a.set(iconPNG)
	if !a.img.Visible() || a.placeholder.Visible() {
		t.Error("tile with art should show the image")
	}
	a.set(nil)
	if a.img.Visible() {
		t.Error("clearing the art should bring back the placeholder")
	}
}

// Every text style gets Inter, which must cover the accented letters common
// in song titles, including Vietnamese.
func TestThemeFonts(t *testing.T) {
	const sample = "Café Señor Ångström Zoë Łódź Ştiinţă Tiếng Việt Đường Phố ơư ạảấầẩẫậắằẳẵặẹẻẽếềểễệỉịọỏốồổỗộớờởỡợụủứừửữựỳỵỷỹ"
	styles := []fyne.TextStyle{{}, {Bold: true}, {Italic: true}, {Bold: true, Italic: true}}
	seen := map[string]bool{}
	for _, s := range styles {
		res := appTheme{}.Font(s)
		if seen[res.Name()] {
			t.Errorf("style %+v reuses font %s", s, res.Name())
		}
		seen[res.Name()] = true
		face, err := font.ParseTTF(bytes.NewReader(res.Content()))
		if err != nil {
			t.Fatalf("style %+v: %v", s, err)
		}
		for _, r := range sample {
			if _, ok := face.NominalGlyph(r); !ok && r != ' ' {
				t.Errorf("%s has no glyph for %q", res.Name(), r)
			}
		}
	}
	if got := (appTheme{}).Font(fyne.TextStyle{Monospace: true}); got != theme.DefaultTheme().Font(fyne.TextStyle{Monospace: true}) {
		t.Errorf("monospace font is %s, want the default", got.Name())
	}
}

// Inter's digits differ in width, so the time labels sit in fixed slots:
// the seek bar must not shift as the elapsed time changes.
func TestSeekBarKeepsStillAsTimeChanges(t *testing.T) {
	u, _, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.togglePlay()
	u.win.Resize(fyne.NewSize(1000, 600))

	var pos fyne.Position
	var size fyne.Size
	for i, at := range []time.Duration{0, 71 * time.Second, 168 * time.Second, 111 * time.Second} {
		u.setSeek(at)
		u.win.Content().Refresh() // lays out again, as a real window would
		p, s := fyne.CurrentApp().Driver().AbsolutePositionForObject(u.seek), u.seek.Size()
		if i > 0 && (p != pos || s != size) {
			t.Errorf("at %v the seek bar is at %v size %v, was %v size %v", at, p, s, pos, size)
		}
		pos, size = p, s
	}
	if w := u.elapsed.Size().Width; w < u.elapsed.MinSize().Width {
		t.Errorf("elapsed label %v wide, needs %v", w, u.elapsed.MinSize().Width)
	}
}
