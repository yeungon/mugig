package ui

// Internet radio: the RADIO section of the sidebar, adding and editing
// stations, and playing them. A station plays in the player bar like a
// song that never ends: LIVE instead of its length, no seeking, and Next
// and Previous move through the stations. Pausing hangs up, and playing
// again joins the broadcast where it is now, as a radio does.

import (
	"errors"
	"fmt"
	"mugig/internal/audio"
	"mugig/internal/library"
	"mugig/internal/radio"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// radioIcon is Material Design's "radio" (Apache 2.0).
var radioIcon = svgIcon("radio.svg", "M3.24 6.15C2.51 6.43 2 7.17 2 8v12c0 1.1.89 2 2 2h16c1.11 0 2-.9 2-2V8c0-1.11-.89-2-2-2H8.3l8.26-3.34L15.88 1 3.24 6.15zM7 20c-1.66 0-3-1.34-3-3s1.34-3 3-3 3 1.34 3 3-1.34 3-3 3zm13-8h-2v-2h-2v2H4V8h16v4z")

// stationState is where a station is in being played.
type stationState int

const (
	stationConnecting stationState = iota
	stationPlaying
	stationReconnecting
	stationPaused
)

// stationNow is the station in the player bar.
type stationNow struct {
	s     *radio.Station
	state stationState
	title string // what the station says is playing, "" if it doesn't
}

// buildRadioSection makes the RADIO heading, with its add button, and the
// list of stations below it.
func (u *ui) buildRadioSection() (header, list fyne.CanvasObject) {
	u.newStationBtn = iconButton(theme.ContentAddIcon(), func() { u.addStation() })
	header = container.NewBorder(nil, nil, sectionLabel("RADIO"), u.newStationBtn)
	u.stationList = container.NewVBox()
	u.rebuildStationList()
	return header, u.stationList
}

// rebuildStationList makes a row for each station, after one is added,
// edited or removed.
func (u *ui) rebuildStationList() {
	if u.stationList == nil {
		return // not built yet
	}
	u.stationList.RemoveAll()
	u.stationRows = make(map[*radio.Station]*folderRow, len(u.stations))
	if len(u.stations) == 0 {
		hint := secondaryLabel("Add an internet radio station with the +, from its stream or playlist link.")
		hint.Wrapping = fyne.TextWrapWord
		hint.SizeName = theme.SizeNameCaptionText
		u.stationList.Add(hint)
	}
	for _, s := range u.stations {
		row := newFolderRow(func() { u.tapStation(s) }, func(at fyne.Position) {
			widget.ShowPopUpMenuAtPosition(u.stationMenu(s), u.win.Canvas(), at)
		})
		u.stationRows[s] = row
		u.stationList.Add(row)
	}
	u.refreshStations()
}

// refreshStations shows each station's state: the one in the player bar
// says how it is going, the others what they sound like or where they are.
func (u *ui) refreshStations() {
	for _, s := range u.stations {
		row := u.stationRows[s]
		if row == nil {
			continue
		}
		icon, detail := radioIcon, s.Format
		if detail == "" {
			detail = linkHost(s.Link)
		}
		if n := u.station; n != nil && n.s == s {
			icon = graphicEqIcon
			switch n.state {
			case stationConnecting:
				detail = "Connecting…"
			case stationReconnecting:
				detail = "Reconnecting…"
			case stationPaused:
				detail = "Paused"
			default:
				detail = "Playing"
				if s.Format != "" {
					detail += " · " + s.Format
				}
			}
		}
		row.show(icon, s.Name, detail, "", true, false)
	}
}

// linkHost is the site a link is on, for a station not yet played.
func linkHost(link string) string {
	u, err := radio.ParseLink(link)
	if err != nil {
		return link
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// stationMenu is a station's right-click menu in the sidebar.
func (u *ui) stationMenu(s *radio.Station) *fyne.Menu {
	return fyne.NewMenu("",
		fyne.NewMenuItem("Play", func() { u.playStation(s) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Edit…", func() { u.editStation(s) }),
		fyne.NewMenuItem("Copy Link", func() { fyne.CurrentApp().Clipboard().SetContent(s.Link) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Remove Station…", func() { u.confirmRemoveStation(s) }),
	)
}

// tapStation plays a station, or carries on with it if it is paused. The
// station already playing carries on as it is.
func (u *ui) tapStation(s *radio.Station) {
	if n := u.station; n != nil && n.s == s && n.state != stationPaused {
		return
	}
	u.playStation(s)
}

// addStation asks for a new station's name and link, adds it, and plays
// it.
func (u *ui) addStation() {
	u.askStation("Add Radio Station", "Add", nil, func(name, link string) {
		s := &radio.Station{Name: name, Link: link}
		u.stations = append(u.stations, s)
		u.stationsChanged()
		u.rebuildStationList()
		u.playStation(s)
	})
}

// editStation changes a station's name or link. A station playing from a
// changed link starts again from it.
func (u *ui) editStation(s *radio.Station) {
	u.askStation("Edit Radio Station", "Save", s, func(name, link string) {
		moved := link != s.Link
		s.Name, s.Link = name, link
		if moved {
			s.Format = "" // not yet heard from the new link
		}
		u.stationsChanged()
		u.refreshStations()
		if n := u.station; n != nil && n.s == s {
			if moved && n.state != stationPaused {
				u.playStation(s)
			} else {
				u.showStation()
			}
		}
	})
}

func (u *ui) confirmRemoveStation(s *radio.Station) {
	dialog.ShowConfirm("Remove Station", fmt.Sprintf("Remove %s from your stations?", s.Name), func(ok bool) {
		if ok {
			u.removeStation(s)
		}
	}, u.win)
}

func (u *ui) removeStation(s *radio.Station) {
	if n := u.station; n != nil && n.s == s {
		u.stop()
	}
	u.stations = slices.DeleteFunc(u.stations, func(x *radio.Station) bool { return x == s })
	u.stationsChanged()
	u.rebuildStationList()
}

// askStation asks for a station's name and link, starting from s's, or
// blank for nil, and calls done with them.
func (u *ui) askStation(title, confirm string, s *radio.Station, done func(name, link string)) {
	name, link := widget.NewEntry(), widget.NewEntry()
	name.PlaceHolder = "BBC World Service"
	link.PlaceHolder = "https://… a stream, or a .m3u8, .pls or .m3u link"
	if s != nil {
		name.SetText(s.Name)
		link.SetText(s.Link)
	}
	name.Validator = func(text string) error {
		if strings.TrimSpace(text) == "" {
			return errors.New("a station needs a name")
		}
		return nil
	}
	link.Validator = func(text string) error {
		_, err := radio.ParseLink(text)
		return err
	}
	note := secondaryLabel("Mugig plays MP3, AAC and Ogg Vorbis streams, from Icecast and Shoutcast servers or HLS playlists.")
	note.Wrapping = fyne.TextWrapWord
	note.SizeName = theme.SizeNameCaptionText
	note.Truncation = fyne.TextTruncateOff
	items := []*widget.FormItem{widget.NewFormItem("Name", name), widget.NewFormItem("Link", link), widget.NewFormItem("", note)}
	d := dialog.NewForm(title, confirm, "Cancel", items, func(ok bool) {
		if ok {
			done(strings.TrimSpace(name.Text), strings.TrimSpace(link.Text))
		}
	}, u.win)
	d.Resize(fyne.NewSize(480, d.MinSize().Height))
	d.Show()
	if s == nil {
		u.win.Canvas().Focus(name)
	}
}

// stationsChanged saves the stations.
func (u *ui) stationsChanged() {
	if err := radio.SaveStations(u.stationsPath, u.stations); err != nil {
		fyne.LogError("saving radio stations", err)
	}
}

// playStation connects to s and plays it, replacing whatever was playing.
// Connecting happens in the background; the player bar says so meanwhile,
// and Pause gives up on it.
func (u *ui) playStation(s *radio.Station) {
	if u.station == nil && u.playing.Path != "" {
		u.saveSession() // the song, to pick up later
	}
	u.stop()
	n := &stationNow{s: s, state: stationConnecting}
	u.station = n
	u.playing = library.Track{Path: s.Link, Title: s.Name}
	u.playBtn.SetIcon(theme.MediaPauseIcon())
	u.prevBtn.Enable()
	u.nextBtn.Enable()
	u.total.SetText("LIVE")
	u.sizeTimes(time.Hour)
	u.setSeek(0)
	u.showArt(nil)
	u.showStation()

	u.async(func() {
		live, err := u.openLive(s.Link, func(info audio.LiveInfo) {
			onUI(func() { u.stationNews(n, info) })
		})
		onUI(func() {
			if u.station != n || n.state != stationConnecting {
				if live != nil {
					live.Close() // given up on meanwhile
				}
				return
			}
			if err == nil {
				err = u.player.PlayLive(live, func(err error) { u.stationEnded(n, err) })
				if err != nil {
					live.Close()
				}
			}
			if err != nil {
				u.stop()
				dialog.ShowError(fmt.Errorf("Couldn't play %s: %w", s.Name, err), u.win)
				return
			}
			n.state = stationPlaying
			if f := liveFormat(live); f != s.Format {
				s.Format = f
				u.stationsChanged()
			}
			u.showStation()
			u.updateOutputInfo()
			u.animateVisualizers()
		})
	})
}

// liveFormat describes a station's sound, such as "AAC · 96 kbps".
func liveFormat(l *audio.Live) string {
	f := l.Codec
	if l.Kbps > 0 {
		f += fmt.Sprintf(" · %d kbps", l.Kbps)
	}
	return f
}

// stationNews shows what a station says as it plays: the song on now, or
// that its connection dropped.
func (u *ui) stationNews(n *stationNow, info audio.LiveInfo) {
	if u.station != n || n.state == stationPaused {
		return
	}
	n.title = info.Title
	switch {
	case info.Reconnecting:
		n.state = stationReconnecting
	case n.state == stationReconnecting:
		n.state = stationPlaying
	}
	u.showStation()
}

// stationEnded runs when a station stops by itself: the broadcast ended,
// or the connection was lost and couldn't be made again.
func (u *ui) stationEnded(n *stationNow, err error) {
	if u.station != n {
		return
	}
	name := n.s.Name
	u.stop()
	if err != nil {
		dialog.ShowError(fmt.Errorf("Stopped playing %s: %w", name, err), u.win)
		return
	}
	u.status.SetText(fmt.Sprintf("%s's broadcast has ended.", name))
}

// pauseStation hangs up, keeping the station in the player bar.
func (u *ui) pauseStation() {
	u.player.Stop()
	u.station.state = stationPaused
	u.paused = true
	u.playBtn.SetIcon(theme.MediaPlayIcon())
	u.showStation()
}

// stepStation plays the station by from the one playing: the next for 1,
// the one before for -1, going round.
func (u *ui) stepStation(by int) {
	i := slices.Index(u.stations, u.station.s)
	if i < 0 || len(u.stations) == 0 {
		return
	}
	u.playStation(u.stations[(i+by+len(u.stations))%len(u.stations)])
}

// leaveStation forgets the station in the player bar, as a song takes
// its place.
func (u *ui) leaveStation() {
	if u.station == nil {
		return
	}
	u.station = nil
	u.refreshStations()
}

// line is the player bar's second line for a station: what is on,
// or how connecting is going.
func (n *stationNow) line() string {
	switch {
	case n.state == stationConnecting:
		return "Connecting…"
	case n.state == stationReconnecting:
		return "Reconnecting…"
	case n.title != "":
		return n.title
	}
	return "Live radio"
}

// showStation shows the station everywhere it is shown: the player bar,
// Now Playing, the sidebar and the system's media controls.
func (u *ui) showStation() {
	n := u.station
	u.playing.Title = n.s.Name
	u.playing.Artist = n.line()
	u.nowTitle.SetText(n.s.Name)
	u.nowArtist.SetText(n.line())
	u.updateVisHeader()
	u.refreshStations()
	u.publishNowPlaying()
}

// detail is Now Playing's line under a station's name.
func (n *stationNow) detail() string {
	parts := []string{n.line()}
	if n.s.Format != "" {
		parts = append(parts, n.s.Format)
	}
	if n.state == stationPaused {
		parts = append(parts, "Paused")
	}
	return strings.Join(parts, " · ")
}

// isLink reports whether path is a web link, a station's, rather than a
// file.
func isLink(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
