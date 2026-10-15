package main

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"mugig/internal/humanize"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// restartThreshold is how far into a track "previous" restarts the current
// track instead of going back one, as most players do.
const restartThreshold = 3 * time.Second

// maxHistory caps how many tracks "previous" can walk back through.
const maxHistory = 1000

type repeatMode int

const (
	repeatOff repeatMode = iota
	repeatAll            // loop the whole list
	repeatOne            // loop the current track
)

// Shuffle and repeat icons from Material Design (Apache 2.0), which Fyne's
// theme doesn't include.
var (
	shuffleIcon   = svgIcon("shuffle.svg", "M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z")
	repeatIcon    = svgIcon("repeat.svg", "M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z")
	repeatOneIcon = svgIcon("repeat_one.svg", "M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4zm-4-2V9h-1l-2 1v1h1.5v4H13z")
)

// svgIcon wraps a 24x24 SVG path as an icon that follows the theme colours.
func svgIcon(name, path string) fyne.Resource {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="` + path + `"/></svg>`
	return theme.NewThemedResource(fyne.NewStaticResource(name, []byte(svg)))
}

// buildTransport creates the playback bar shown under the track list:
// album art, now-playing line, transport buttons, seek bar with times, and
// volume.
func (u *ui) buildTransport() fyne.CanvasObject {
	u.art = newArtTile(56)
	u.nowTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	u.nowTitle.Truncation = fyne.TextTruncateEllipsis
	u.nowArtist = secondaryLabel("")
	u.barBadge = newQualityBadge(func() { u.showVisualizer(true) })
	u.barArtistRow = container.NewBorder(nil, nil, u.barBadge, nil, u.nowArtist)

	u.shuffleBtn = iconButton(shuffleIcon, u.toggleShuffle)
	u.prevBtn = iconButton(theme.MediaSkipPreviousIcon(), u.prev)
	u.playBtn = newRoundButton(theme.MediaPlayIcon(), 40, u.togglePlay)
	u.nextBtn = iconButton(theme.MediaSkipNextIcon(), u.next)
	u.repeatBtn = iconButton(repeatIcon, u.cycleRepeat)
	u.shuffle = u.prefs.Bool(prefShuffle)
	u.repeat = repeatMode(u.prefs.Int(prefRepeat))
	u.updateModeButtons()

	u.elapsed = timeLabel("", fyne.TextAlignTrailing)
	u.total = timeLabel("", fyne.TextAlignLeading)
	for i := range u.timeSlots {
		u.timeSlots[i] = canvas.NewRectangle(color.Transparent)
	}

	// The seek bar works in seconds. SetValue fires the same callbacks as a
	// user drag, so settingSeek marks our own updates and they are ignored.
	u.seek = widget.NewSlider(0, 1)
	u.seek.OnChanged = func(v float64) {
		if u.settingSeek {
			return
		}
		u.seeking = true // preview the time while dragging
		u.elapsed.SetText(humanize.Clock(seconds(v)))
	}
	u.seek.OnChangeEnded = func(v float64) {
		if u.settingSeek {
			return
		}
		u.seeking = false
		u.seekTo(seconds(v))
	}

	u.volume = widget.NewSlider(0, 1)
	u.volume.Step = 0.01
	u.muteBtn = iconButton(theme.VolumeUpIcon(), u.toggleMute)
	u.volume.OnChanged = u.volumeChanged
	u.volume.OnChangeEnded = func(v float64) { u.prefs.SetFloat(prefVolume, v) }
	// Set directly rather than with SetValue, which skips the callback when
	// the value is unchanged (a saved volume of 0).
	u.volume.Value = u.prefs.FloatWithFallback(prefVolume, 0.8)
	u.volumeChanged(u.volume.Value)
	u.visBtn = iconButton(graphicEqIcon, u.toggleVisualizer)

	mini := u.buildMiniVisualizer()
	if !u.prefs.BoolWithFallback(prefMiniVis, true) {
		u.miniView.Hide()
	}
	u.miniItem.Checked = u.miniView.Visible()

	u.setStopped()

	track := container.NewBorder(nil, nil, u.art.box, nil,
		container.NewVBox(layout.NewSpacer(), u.nowTitle, u.barArtistRow, layout.NewSpacer()))
	controls := container.NewVBox(
		container.NewCenter(container.NewHBox(u.shuffleBtn, u.prevBtn, u.playBtn, u.nextBtn, u.repeatBtn)),
		container.NewBorder(nil, nil,
			container.NewStack(u.timeSlots[0], u.elapsed),
			container.NewStack(u.timeSlots[1], u.total),
			u.seek),
	)
	u.queueBtn = iconButton(queueIcon, u.showQueue)
	volume := container.NewHBox(layout.NewSpacer(), container.NewCenter(u.buildSpeedButton()), u.queueBtn, u.visBtn, u.muteBtn,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(110, u.volume.MinSize().Height), u.volume)),
		mini)

	return container.NewStack(
		newSurface(colorNameSurface),
		container.NewBorder(widget.NewSeparator(), nil, nil, nil,
			container.NewPadded(container.New(playerBarLayout{}, track, controls, volume))),
	)
}

// jump plays t as a step forward, remembering the current track so
// "previous" can return to it in shuffle mode.
func (u *ui) jump(t Track) {
	u.remember()
	u.play(t)
}

// remember adds the playing track to the history "previous" walks back
// through in shuffle mode.
func (u *ui) remember() {
	if u.playing.Path != "" {
		u.history = append(u.history, u.playing.Path)
		if len(u.history) > maxHistory {
			u.history = u.history[1:]
		}
	}
}

// play starts t, from the list or a file opened, replacing whatever was
// playing. Called on the UI thread.
func (u *ui) play(t Track) {
	u.start(t, false)
}

// start plays t. queued says it comes from Up Next, so the list carries on
// afterwards from where it was rather than from t.
func (u *ui) start(t Track, queued bool) {
	if err := u.player.Play(t.Path, u.trackEnded); err != nil {
		u.setStopped()
		dialog.ShowError(err, u.win)
		return
	}
	u.playing = t
	u.paused = false
	u.cued = nil
	u.fromQueue = queued
	if !queued {
		u.listPath = t.Path
	}
	// Remembered now, not only on quit, in case Mugig doesn't get to quit
	// normally; the position is saved on pause and quit.
	u.prefs.SetString(prefResumeSong, t.Path)
	u.prefs.SetFloat(prefResumeAt, 0)
	if u.queueValid {
		u.queue = slices.DeleteFunc(u.queue, func(p string) bool { return p == t.Path })
	}
	u.playBtn.SetIcon(theme.MediaPauseIcon())
	u.prevBtn.Enable()
	u.nextBtn.Enable()
	u.nowTitle.SetText(t.Title)
	u.nowArtist.SetText(t.Artist)
	u.table.Refresh() // move the playing marker

	u.seeking = false
	if d := u.player.Duration(); d > 0 {
		u.seek.Max = d.Seconds()
		u.seek.Enable()
		u.total.SetText(humanize.Clock(d))
	} else {
		u.seek.Disable() // length unknown, so seeking isn't possible
		u.total.SetText("--:--")
	}
	u.sizeTimes(u.player.Duration())
	u.setSeek(0)
	u.revealPlaying()
	u.updateVisHeader()
	u.loadQuality(t)
	u.updateOutputInfo()
	u.animateVisualizers()

	u.showArt(nil)
	u.async(func() { // reading art can be slow on a spinning disk
		data := coverArt(t.Path)
		onUI(func() {
			if u.playing.Path == t.Path { // still the same track
				u.showArt(data)
				u.publishNowPlaying()
			}
		})
	})
	u.publishNowPlaying()
}

// showArt shows the cover image data, or a placeholder for nil.
func (u *ui) showArt(data []byte) {
	u.artData = data
	u.art.set(data)
	u.bigArt.set(data)
}

// togglePlay pauses or resumes, or if nothing is playing starts Up Next or
// the list.
func (u *ui) togglePlay() {
	if u.playing.Path == "" {
		if u.cued != nil {
			u.playCued()
		} else {
			u.advance()
		}
		return
	}
	u.paused = u.player.TogglePause()
	if u.paused {
		u.saveSession()
	}
	if u.paused {
		u.playBtn.SetIcon(theme.MediaPlayIcon())
	} else {
		u.playBtn.SetIcon(theme.MediaPauseIcon())
		u.animateVisualizers()
	}
	u.publishNowPlaying()
}

func (u *ui) stop() {
	u.player.Stop()
	u.setStopped()
}

// trackEnded runs when a track ends, giving continuous playback. If it
// stopped early because it couldn't be read, playback stops and says why,
// rather than moving on as if the song had finished.
func (u *ui) trackEnded(err error) {
	if err != nil {
		title := u.playing.Title
		u.stop()
		dialog.ShowError(fmt.Errorf("Stopped playing %s: %w", title, err), u.win)
		return
	}
	if u.repeat == repeatOne {
		u.start(u.playing, u.fromQueue)
		return
	}
	u.next()
}

// next plays the following track, or stops at the end of the list. Unlike a
// track ending, pressing next moves on even in repeat-one mode.
func (u *ui) next() {
	if !u.advance() {
		u.stop()
	}
}

// advance plays the first song in Up Next, or else the list's next track.
// It reports false if there is neither.
func (u *ui) advance() bool {
	if t, ok := u.takeQueued(); ok {
		u.remember()
		u.start(t, true)
		return true
	}
	if t, ok := u.upNext(); ok {
		u.jump(t)
		return true
	}
	return false
}

// upNext picks the list's track after the current one, following shuffle
// and repeat. ok is false when playback should stop.
func (u *ui) upNext() (t Track, ok bool) {
	if u.shuffle && len(u.tracks) > 0 {
		return u.nextShuffled()
	}
	return u.listNext()
}

// listNext is the track after the current one in list order.
func (u *ui) listNext() (t Track, ok bool) {
	if len(u.tracks) == 0 {
		return Track{}, false
	}
	i := u.listIndex() // -1 if nothing is playing, so this starts at the top
	switch {
	case i+1 < len(u.tracks):
		return u.tracks[i+1], true
	case u.repeat == repeatAll:
		return u.tracks[0], true
	}
	return Track{}, false
}

// nextShuffled plays the list in random order, each track once per round.
// A new round starts only in repeat-all mode.
func (u *ui) nextShuffled() (Track, bool) {
	if !u.queueValid {
		u.refillQueue(false)
	}
	for {
		if len(u.queue) == 0 {
			if u.repeat != repeatAll {
				return Track{}, false
			}
			u.refillQueue(true)
			if len(u.queue) == 0 {
				return Track{}, false
			}
		}
		p := u.queue[0]
		u.queue = u.queue[1:]
		if i := u.indexOf(p); i >= 0 { // skip tracks no longer listed
			return u.tracks[i], true
		}
	}
}

// following is the song that will play when the current one ends, as
// trackEnded would choose it, without taking it from Up Next or the
// shuffle round. ok is false if playback would stop, or the song can't be
// known yet.
func (u *ui) following() (t Track, ok bool) {
	if u.repeat == repeatOne {
		return u.playing, true
	}
	for _, q := range u.queued {
		if t, ok := u.inLibrary(q.Path); ok {
			return t, true
		}
	}
	if !u.shuffle {
		return u.listNext()
	}
	if !u.queueValid {
		u.refillQueue(false)
	}
	for _, p := range u.queue {
		if i := u.indexOf(p); i >= 0 {
			return u.tracks[i], true
		}
	}
	if u.repeat != repeatAll {
		return Track{}, false
	}
	u.refillQueue(true) // the round is over: shuffle the next now, as nextShuffled would
	if len(u.queue) == 0 {
		return Track{}, false
	}
	return u.tracks[u.indexOf(u.queue[0])], true
}

// prepareNext readies the song that follows, once the current one is
// near its end, so the two join without a gap. Asked again as the end
// nears, it follows changes to Up Next, shuffle, repeat or the list.
func (u *ui) prepareNext() {
	if u.playing.Path == "" {
		return
	}
	if d := u.player.Duration(); d <= 0 || d-u.player.Position() > gaplessLead {
		return
	}
	t, _ := u.following()
	u.player.Prepare(t.Path)
}

// refillQueue shuffles the listed tracks into a new round. When a round is
// being (re)started mid-way, the playing track counts as already played and
// is left out. A follow-on round includes it, just not first, so it doesn't
// play twice in a row.
func (u *ui) refillQueue(followOn bool) {
	u.queue = u.queue[:0]
	for _, t := range u.tracks {
		if followOn || t.Path != u.playing.Path {
			u.queue = append(u.queue, t.Path)
		}
	}
	rand.Shuffle(len(u.queue), func(i, j int) {
		u.queue[i], u.queue[j] = u.queue[j], u.queue[i]
	})
	if n := len(u.queue); n > 1 && u.queue[0] == u.playing.Path {
		u.queue[0], u.queue[n-1] = u.queue[n-1], u.queue[0]
	}
	u.queueValid = true
}

// prev restarts the current track if it is under way. Otherwise it goes
// back: through play history in shuffle mode, or up the list.
func (u *ui) prev() {
	if u.player.Position() >= restartThreshold {
		u.restart()
		return
	}
	if u.shuffle {
		for len(u.history) > 0 {
			p := u.history[len(u.history)-1]
			u.history = u.history[:len(u.history)-1]
			if i := u.indexOf(p); i >= 0 {
				if u.queueValid { // so "next" comes back to this track
					u.queue = slices.Insert(u.queue, 0, u.playing.Path)
				}
				u.play(u.tracks[i])
				return
			}
		}
		u.restart()
		return
	}
	if u.fromQueue { // back to the list, where it was before Up Next
		if i := u.indexOf(u.listPath); i >= 0 {
			u.play(u.tracks[i])
			return
		}
	}
	i := u.playingIndex()
	switch {
	case i > 0:
		u.play(u.tracks[i-1])
	case i == 0 && u.repeat == repeatAll && len(u.tracks) > 1:
		u.play(u.tracks[len(u.tracks)-1])
	default:
		u.restart()
	}
}

func (u *ui) restart() {
	u.seekTo(0)
}

// seekTo jumps to d within the playing track.
func (u *ui) seekTo(d time.Duration) {
	if u.playing.Path == "" {
		if u.cued != nil { // moves where Play will start
			u.cued.at = min(max(d, 0), u.cued.track.Length)
			u.setSeek(u.cued.at)
		}
		return
	}
	d = max(d, 0)
	if total := u.player.Duration(); total > 0 {
		d = min(d, total)
	}
	if err := u.player.Seek(d); err != nil {
		dialog.ShowError(err, u.win)
	}
	u.setSeek(d)
	u.publishNowPlaying()
}

func (u *ui) toggleShuffle() {
	u.shuffle = !u.shuffle
	u.queueValid = false
	u.history = nil
	u.prefs.SetBool(prefShuffle, u.shuffle)
	u.updateModeButtons()
}

// cycleRepeat steps through off, repeat all and repeat one.
func (u *ui) cycleRepeat() {
	u.setRepeatMode((u.repeat + 1) % 3)
}

// setRepeat turns mode on, or off if it is already on (for the menu).
func (u *ui) setRepeat(mode repeatMode) {
	if u.repeat == mode {
		mode = repeatOff
	}
	u.setRepeatMode(mode)
}

func (u *ui) setRepeatMode(mode repeatMode) {
	u.repeat = mode
	u.prefs.SetInt(prefRepeat, int(u.repeat))
	u.updateModeButtons()
}

// updateModeButtons shows whether shuffle and repeat are on, in the bar and
// in the menu.
func (u *ui) updateModeButtons() {
	u.shuffleItem.Checked = u.shuffle
	u.repeatAllItem.Checked = u.repeat == repeatAll
	u.repeatOneItem.Checked = u.repeat == repeatOne
	u.mainMenu.Refresh()

	setToggle(u.shuffleBtn, shuffleIcon, u.shuffle)
	if u.repeat == repeatOne {
		setToggle(u.repeatBtn, repeatOneIcon, true)
	} else {
		setToggle(u.repeatBtn, repeatIcon, u.repeat == repeatAll)
	}
}

func (u *ui) volumeChanged(v float64) {
	u.player.SetVolume(v)
	if v > 0 {
		u.muteBtn.SetIcon(theme.VolumeUpIcon())
	} else {
		u.muteBtn.SetIcon(theme.VolumeMuteIcon())
	}
}

func (u *ui) toggleMute() {
	if u.volume.Value > 0 {
		u.unmuteLevel = u.volume.Value
		u.volume.SetValue(0)
		return
	}
	if u.unmuteLevel == 0 {
		u.unmuteLevel = 0.8
	}
	u.volume.SetValue(u.unmuteLevel)
}

// setStopped resets the playback controls. It also clears the table
// selection, since Fyne doesn't fire OnSelected for an already-selected
// cell and the user should be able to click the same row to play it again.
func (u *ui) setStopped() {
	u.playing = Track{}
	u.paused = false
	u.cued = nil
	u.fromQueue = false
	u.queueValid = false // the next play starts a fresh shuffle round
	u.history = nil
	u.playBtn.SetIcon(theme.MediaPlayIcon())
	u.prevBtn.Disable()
	u.nextBtn.Disable()
	u.nowTitle.SetText("Not playing")
	u.nowArtist.SetText("")
	u.table.Refresh() // clear the playing marker
	u.seeking = false
	u.setSeek(0)
	u.seek.Disable()
	u.total.SetText(humanize.Clock(0))
	u.sizeTimes(0)
	u.table.UnselectAll()
	u.updateVisHeader()
	u.loadQuality(Track{})
	u.updateOutputInfo()
	u.showArt(nil)
	u.publishNowPlaying()
}

// trackProgress moves the seek bar while a track plays. It runs for the
// life of the app.
func (u *ui) trackProgress() {
	for range time.Tick(250 * time.Millisecond) {
		onUI(func() {
			if u.playing.Path != "" && !u.seeking {
				u.setSeek(u.player.Position())
			}
			u.prepareNext()
		})
	}
}

func (u *ui) setSeek(pos time.Duration) {
	u.settingSeek = true
	u.seek.SetValue(pos.Seconds())
	u.settingSeek = false
	u.elapsed.SetText(humanize.Clock(pos))
}

// revealPlaying scrolls the playing track's row into view, if it is in the
// list.
func (u *ui) revealPlaying() {
	if i := u.playingIndex(); i >= 0 {
		u.table.ScrollTo(widget.TableCellID{Row: i, Col: 0})
	}
}

// playingIndex finds the playing track in the list, or returns -1.
func (u *ui) playingIndex() int {
	if u.playing.Path == "" {
		return -1
	}
	return u.indexOf(u.playing.Path)
}

// listIndex is where the list carries on from: the playing track, or while
// a song from Up Next plays, the last track played from the list. It is -1
// if neither is listed.
func (u *ui) listIndex() int {
	if u.fromQueue {
		return u.indexOf(u.listPath)
	}
	return u.playingIndex()
}

// indexOf finds a track in the list by path, since rows move when the list
// is sorted, filtered or rescanned.
func (u *ui) indexOf(path string) int {
	for i, t := range u.tracks {
		if t.Path == path {
			return i
		}
	}
	return -1
}

func trackLabel(t Track) string {
	if t.Artist == "" {
		return t.Title
	}
	return t.Title + " — " + t.Artist
}

func seconds(v float64) time.Duration {
	return time.Duration(v * float64(time.Second))
}

// timeLabel shows a time next to the seek bar.
func timeLabel(text string, align fyne.TextAlign) *widget.Label {
	l := widget.NewLabelWithStyle(text, align, fyne.TextStyle{})
	l.Importance = widget.LowImportance
	l.SizeName = theme.SizeNameCaptionText
	return l
}

// sizeTimes makes the time labels wide enough for any time up to d, so
// the seek bar keeps still while a track plays even though the font's
// digits differ in width.
func (u *ui) sizeTimes(d time.Duration) {
	widest, w := '0', float32(0)
	for r := '0'; r <= '9'; r++ {
		if rw := fyne.MeasureText(string(r), theme.CaptionTextSize(), fyne.TextStyle{}).Width; rw > w {
			widest, w = r, rw
		}
	}
	sample := []rune(humanize.Clock(max(d, 0)))
	for i, r := range sample {
		if r >= '0' && r <= '9' {
			sample[i] = widest
		}
	}
	width := max(timeLabel(string(sample), 0).MinSize().Width, timeLabel("--:--", 0).MinSize().Width)
	for _, r := range u.timeSlots {
		r.SetMinSize(fyne.NewSize(width, 0))
		r.Refresh()
	}
}

// audioPlayer is what the UI needs from playback. The real one is player;
// tests use a fake so they make no sound.
type audioPlayer interface {
	Play(path string, onEnd func(error)) error
	TogglePause() bool
	Stop()
	Position() time.Duration
	Duration() time.Duration
	Seek(d time.Duration) error
	// Prepare readies the song at path to follow the current one without a
	// gap, or none for "". Once it has taken over, Play with its path
	// carries on with it rather than starting it again.
	Prepare(path string)
	SetVolume(level float64)
	// SetFadeIn turns fading in from near silence, when playing starts or
	// resumes, on or off.
	SetFadeIn(on bool)
	SetSpeed(speed float64) // 1 is normal; the pitch is kept
	SetCrossfeed(level crossfeedLevel)
	SetEQ(s eqSettings)
	// Samples fills dst with the most recent audio, mono and oldest first,
	// and returns its sample rate, or 0 if nothing is playing.
	Samples(dst []float64) int
	// Output describes where the sound goes: the device's rate, and the
	// track's rate, which differ if the audio is converted. 0s if stopped.
	Output() (deviceRate, trackRate int)
	// Devices lists the output devices, and SetDevice picks one by ID, ""
	// for the system default. SetMatchRate turns switching the device to
	// each track's rate on or off.
	Devices() []outputDevice
	SetDevice(id string)
	SetMatchRate(on bool)
	// SetExclusive turns taking the device for Mugig alone on or off, and
	// Exclusive reports whether Mugig has it now.
	SetExclusive(on bool)
	Exclusive() bool
	// DevicesChanged is called when devices come or go, or the system's
	// default output changes.
	DevicesChanged()
}
