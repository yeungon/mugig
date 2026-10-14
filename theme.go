package main

// The app's look: a custom theme in the icon's violet, designed dark-first
// with a matching light variant that follows the system setting, plus a few
// small styled widgets the default theme has no equivalent for.

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed IconRounded.png
var iconPNG []byte

// appIcon is the rounded icon: the window and taskbar icon on Windows and
// Linux, and the sidebar's logo. (macOS takes the Dock icon from the app
// bundle, made from Icon.png; see tools/mkicon and dock_darwin.go.)
var appIcon = fyne.NewStaticResource("IconRounded.png", iconPNG)

// colorNameSurface is for panels that sit on the background: the sidebar
// and the player bar.
const colorNameSurface fyne.ThemeColorName = "surface"

// colorNameAccent is for violet text and icons (the playing track, active
// shuffle/repeat). Primary is for fills, such as the play button, and must
// be darker so white on it stays readable; accent must be lighter so it
// stays readable on the dark background.
const colorNameAccent fyne.ThemeColorName = "accent"

type appTheme struct{}

var _ fyne.Theme = appTheme{}

func hex(v uint32) color.NRGBA {
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF}
}

func alphaOf(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

var (
	violet      = hex(0x7C3AED) // fills; white text on it is 5.7:1
	violetLight = hex(0xA78BFA) // accent text on the dark background, 6.9:1
	white       = hex(0xFFFFFF)
	black       = hex(0x000000)
	darkPalette = map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0x111116),
		colorNameSurface:                   hex(0x18181F),
		theme.ColorNameHeaderBackground:    hex(0x18181F),
		theme.ColorNameMenuBackground:      hex(0x1F1F27),
		theme.ColorNameOverlayBackground:   hex(0x1F1F27),
		theme.ColorNameInputBackground:     hex(0x22222B),
		theme.ColorNameInputBorder:         hex(0x30303C),
		theme.ColorNameButton:              hex(0x26262F),
		theme.ColorNameDisabledButton:      hex(0x1E1E25),
		theme.ColorNameForeground:          hex(0xECECF1),
		theme.ColorNameDisabled:            hex(0x8A8A99), // also used for secondary text
		theme.ColorNamePlaceHolder:         hex(0x7E7E8E),
		theme.ColorNamePrimary:             violet,
		colorNameAccent:                    violetLight,
		theme.ColorNameForegroundOnPrimary: white,
		theme.ColorNameHyperlink:           violetLight,
		theme.ColorNameHover:               alphaOf(white, 0x12),
		theme.ColorNamePressed:             alphaOf(white, 0x22),
		theme.ColorNameFocus:               alphaOf(violet, 0x80),
		theme.ColorNameSelection:           alphaOf(violet, 0x40),
		theme.ColorNameSeparator:           hex(0x24242D),
		theme.ColorNameScrollBar:           alphaOf(white, 0x30),
		theme.ColorNameShadow:              alphaOf(black, 0x70),
		theme.ColorNameError:               hex(0xF43F5E),
		theme.ColorNameWarning:             hex(0xF59E0B),
		theme.ColorNameSuccess:             hex(0x22C55E),
	}
	lightPalette = map[fyne.ThemeColorName]color.Color{
		theme.ColorNameBackground:          hex(0xF7F7FA),
		colorNameSurface:                   hex(0xEEEEF3),
		theme.ColorNameHeaderBackground:    hex(0xEEEEF3),
		theme.ColorNameMenuBackground:      white,
		theme.ColorNameOverlayBackground:   white,
		theme.ColorNameInputBackground:     white,
		theme.ColorNameInputBorder:         hex(0xD9D9E3),
		theme.ColorNameButton:              hex(0xE4E4EC),
		theme.ColorNameDisabledButton:      hex(0xECECF1),
		theme.ColorNameForeground:          hex(0x1A1A22),
		theme.ColorNameDisabled:            hex(0x6B6B7B),
		theme.ColorNamePlaceHolder:         hex(0x7A7A88),
		theme.ColorNamePrimary:             violet,
		colorNameAccent:                    violet, // 5.3:1 on the light background
		theme.ColorNameForegroundOnPrimary: white,
		theme.ColorNameHyperlink:           violet,
		theme.ColorNameHover:               alphaOf(black, 0x0D),
		theme.ColorNamePressed:             alphaOf(black, 0x1A),
		theme.ColorNameFocus:               alphaOf(violet, 0x80),
		theme.ColorNameSelection:           alphaOf(violet, 0x2A),
		theme.ColorNameSeparator:           hex(0xE2E2EA),
		theme.ColorNameScrollBar:           alphaOf(black, 0x30),
		theme.ColorNameShadow:              alphaOf(black, 0x30),
		theme.ColorNameError:               hex(0xBE123C),
		theme.ColorNameWarning:             hex(0xA14A06),
		theme.ColorNameSuccess:             hex(0x15803D),
	}
	sizes = map[fyne.ThemeSizeName]float32{
		theme.SizeNameText:               13,
		theme.SizeNameHeadingText:        24,
		theme.SizeNameSubHeadingText:     16,
		theme.SizeNameCaptionText:        11,
		theme.SizeNameInputRadius:        8,
		theme.SizeNameSelectionRadius:    6,
		theme.SizeNameScrollBar:          8,
		theme.SizeNameScrollBarSmall:     3,
		theme.SizeNameScrollBarRadius:    4,
		theme.SizeNameSeparatorThickness: 1,
	}
)

func (appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := darkPalette
	if v == theme.VariantLight {
		p = lightPalette
	}
	if c, ok := p[n]; ok {
		return c
	}
	return theme.DefaultTheme().Color(n, v)
}

func (appTheme) Size(n fyne.ThemeSizeName) float32 {
	if s, ok := sizes[n]; ok {
		return s
	}
	return theme.DefaultTheme().Size(n)
}

// Text is set in Inter (SIL Open Font License, see fonts/LICENSE-Inter.txt),
// a typeface designed for screens. Bold uses its SemiBold weight, which
// reads as emphasis without looking heavy. Monospace and symbols keep the
// default fonts, and characters Inter lacks, such as CJK, fall back to the
// system's fonts.
var (
	//go:embed fonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed fonts/Inter-SemiBold.ttf
	interBold []byte
	//go:embed fonts/Inter-Italic.ttf
	interItalic []byte
	//go:embed fonts/Inter-SemiBoldItalic.ttf
	interBoldItalic []byte

	fontRegular    = fyne.NewStaticResource("Inter-Regular.ttf", interRegular)
	fontBold       = fyne.NewStaticResource("Inter-SemiBold.ttf", interBold)
	fontItalic     = fyne.NewStaticResource("Inter-Italic.ttf", interItalic)
	fontBoldItalic = fyne.NewStaticResource("Inter-SemiBoldItalic.ttf", interBoldItalic)
)

func (appTheme) Font(s fyne.TextStyle) fyne.Resource {
	switch {
	case s.Monospace, s.Symbol:
		return theme.DefaultTheme().Font(s)
	case s.Bold && s.Italic:
		return fontBoldItalic
	case s.Bold:
		return fontBold
	case s.Italic:
		return fontItalic
	}
	return fontRegular
}

func (appTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

// themeColor looks up a colour in the current theme and variant.
func themeColor(n fyne.ThemeColorName) color.Color {
	a := fyne.CurrentApp()
	return a.Settings().Theme().Color(n, a.Settings().ThemeVariant())
}

// ---------------------------------------------------------------------------
// Styled widgets
// ---------------------------------------------------------------------------

// surface is a panel background in a theme colour that keeps up when the
// system switches between light and dark.
type surface struct {
	widget.BaseWidget
	color  fyne.ThemeColorName
	radius float32
}

func newSurface(c fyne.ThemeColorName) *surface {
	return newRoundedSurface(c, 0)
}

func newRoundedSurface(c fyne.ThemeColorName, radius float32) *surface {
	s := &surface{color: c, radius: radius}
	s.ExtendBaseWidget(s)
	return s
}

func (s *surface) CreateRenderer() fyne.WidgetRenderer {
	r := &surfaceRenderer{s: s, rect: canvas.NewRectangle(nil)}
	r.Refresh()
	return r
}

type surfaceRenderer struct {
	s    *surface
	rect *canvas.Rectangle
}

func (r *surfaceRenderer) Layout(size fyne.Size)        { r.rect.Resize(size) }
func (r *surfaceRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (r *surfaceRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect} }
func (r *surfaceRenderer) Destroy()                     {}

// Refresh picks up the colour again, as Fyne refreshes everything when the
// theme or light/dark variant changes.
func (r *surfaceRenderer) Refresh() {
	r.rect.FillColor = themeColor(r.s.color)
	r.rect.CornerRadius = r.s.radius
	r.rect.Refresh()
}

// roundButton is the big circular play/pause button.
type roundButton struct {
	widget.BaseWidget
	icon    fyne.Resource
	onTap   func()
	size    float32
	hovered bool
}

func newRoundButton(icon fyne.Resource, size float32, onTap func()) *roundButton {
	b := &roundButton{icon: icon, onTap: onTap, size: size}
	b.ExtendBaseWidget(b)
	return b
}

func (b *roundButton) SetIcon(icon fyne.Resource) {
	b.icon = icon
	b.Refresh()
}

func (b *roundButton) Tapped(*fyne.PointEvent) { b.onTap() }
func (b *roundButton) Cursor() desktop.Cursor  { return desktop.PointerCursor }

func (b *roundButton) MouseIn(*desktop.MouseEvent)    { b.hovered = true; b.Refresh() }
func (b *roundButton) MouseMoved(*desktop.MouseEvent) {}
func (b *roundButton) MouseOut()                      { b.hovered = false; b.Refresh() }

func (b *roundButton) MinSize() fyne.Size { return fyne.NewSize(b.size, b.size) }

func (b *roundButton) CreateRenderer() fyne.WidgetRenderer {
	r := &roundButtonRenderer{b: b, circle: canvas.NewCircle(nil), img: canvas.NewImageFromResource(nil)}
	r.img.FillMode = canvas.ImageFillContain
	r.Refresh()
	return r
}

type roundButtonRenderer struct {
	b      *roundButton
	circle *canvas.Circle
	img    *canvas.Image
}

func (r *roundButtonRenderer) Layout(s fyne.Size) {
	d := min(s.Width, s.Height)
	r.circle.Resize(fyne.NewSize(d, d))
	r.circle.Move(fyne.NewPos((s.Width-d)/2, (s.Height-d)/2))
	icon := d * 0.5
	r.img.Resize(fyne.NewSize(icon, icon))
	r.img.Move(fyne.NewPos((s.Width-icon)/2, (s.Height-icon)/2))
}

func (r *roundButtonRenderer) MinSize() fyne.Size { return r.b.MinSize() }

func (r *roundButtonRenderer) Refresh() {
	fill := themeColor(theme.ColorNamePrimary)
	if r.b.hovered { // a touch brighter on hover
		fill = blend(toNRGBA(fill), hex(0xFFFFFF), 0.15)
	}
	r.circle.FillColor = fill
	r.circle.Refresh()
	r.img.Resource = theme.NewColoredResource(r.b.icon, theme.ColorNameForegroundOnPrimary)
	r.img.Refresh()
}

func (r *roundButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.img}
}

func (r *roundButtonRenderer) Destroy() {}

func toNRGBA(c color.Color) color.NRGBA {
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// iconButton is a flat, borderless icon button, as used for the transport
// controls.
func iconButton(icon fyne.Resource, onTap func()) *widget.Button {
	b := widget.NewButtonWithIcon("", icon, onTap)
	b.Importance = widget.LowImportance
	return b
}

// setToggle shows whether a flat icon button's mode is on by colouring its
// icon with the accent.
func setToggle(b *widget.Button, icon fyne.Resource, on bool) {
	if on {
		b.SetIcon(theme.NewColoredResource(icon, colorNameAccent))
	} else {
		b.SetIcon(icon)
	}
}

// isToggledOn reports whether setToggle last turned b on.
func isToggledOn(b *widget.Button) bool {
	r, ok := b.Icon.(*theme.ThemedResource)
	return ok && r.ColorName == colorNameAccent
}

// sectionLabel is a small upper-case heading, as used in the sidebar.
func sectionLabel(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = theme.SizeNameCaptionText
	l.Importance = widget.LowImportance
	return l
}

// secondaryLabel is muted text for supporting details.
func secondaryLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}
