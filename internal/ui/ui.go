// package ui is Mugig's window, made with Fyne: the sidebar of folders and
// playlists, the song list, the player bar, Now Playing, the quality
// check's window, the equalizer and the menus. Run opens it.
//
// The work itself is done by the packages beside it: library finds the
// songs, audio plays them, and quality checks them. Scans and checks run
// in the background and hand their results to the UI thread (see onUI).
//
// Requires Fyne v2.6+ (for fyne.Do).
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mugig/internal/audio"
	"mugig/internal/humanize"
	"mugig/internal/library"
	"mugig/internal/quality"
	"mugig/internal/trash"
)

var columns = []string{"Title", "Artist", "Album", "Time", "Year", "Format", "Size", "Quality", "Path"}

// Each column's index in columns.
const (
	titleCol = iota
	artistCol
	albumCol
	timeCol
	yearCol
	formatCol
	sizeCol
	qualityCol
	pathCol
)

// appID identifies the app to the OS; preferences are stored under it, so
// changing it forgets saved settings. It keeps the app's original name,
// Music Scanner, for that reason.
const appID = "org.kiwipanel.musicscan"

// Preference keys for settings kept between launches.
const (
	prefFolder   = "lastFolder" // the one music folder of earlier versions (see prefFolders)
	prefVolume   = "volume"
	prefShuffle  = "shuffle"
	prefRepeat   = "repeat"
	prefSortCol  = "sortColumnName" // the column's name, or "none"
	prefSortDesc = "sortDescending"
	prefVisMode  = "visualizerMode"
	prefMiniVis  = "miniVisualizer"
)

// ---------------------------------------------------------------------------
// Scanning (runs entirely off the UI thread)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// GUI
// ---------------------------------------------------------------------------

type ui struct {
	win       fyne.Window
	prefs     fyne.Preferences
	cache     *library.Cache
	media     mediaCenter
	async     func(func())            // runs slow work off the UI thread
	pick      picker                  // shows an Open panel (see open.go)
	save      saver                   // shows a Save panel
	reveal    func(path string) error // shows a file in Finder or Explorer; tests replace it
	modifiers func() fyne.KeyModifier // the modifier keys held down; tests replace it
	trash     func(path string) error // moves a file to the Trash; tests replace it
	mainMenu  *fyne.MainMenu
	table     *trackTable
	search    *widget.Entry
	count     *widget.Label
	status    *widget.Label
	progress  *widget.ProgressBarInfinite
	addBtn    *widget.Button
	rescanBtn *widget.Button
	cancelBtn *widget.Button

	// Layout (see layout.go).
	songsPage     *fyne.Container
	pageTitle     *widget.TextSegment // "Songs", or the folder shown
	navSongs      *widget.Button
	navNowPlaying *widget.Button
	folderList    *fyne.Container // the library's folders, in the sidebar

	// Playback (see transport.go).
	player         audioPlayer
	playing        library.Track // zero when stopped
	prevBtn        *widget.Button
	playBtn        *roundButton
	nextBtn        *widget.Button
	nowTitle       *widget.Label
	nowArtist      *widget.Label
	seek           *widget.Slider
	elapsed        *widget.Label
	total          *widget.Label
	timeSlots      [2]*canvas.Rectangle // keep the time labels a steady width
	barBadge       *qualityBadge        // lossless or not, in the player bar
	np             *qualityPanel        // the playing track's quality, on the Now Playing page
	barArtistRow   *fyne.Container      // barBadge and the artist
	npQuality      *fyne.Container      // npChecks and checkBtn
	npOutput       *widget.Label        // where the sound goes, on the Now Playing page
	outputItem     *fyne.MenuItem       // the Output Device submenu
	matchRateItem  *fyne.MenuItem
	exclusiveItem  *fyne.MenuItem
	canHog         bool           // the system offers exclusive access to a device
	checkBtn       *widget.Button // Check Audio Quality, on the Now Playing page
	checkItem      *fyne.MenuItem // the same in the menu
	quality        trackQuality   // the playing track's (see qualityview.go)
	qualityStore   *quality.Store
	volume         *widget.Slider
	muteBtn        *widget.Button
	shuffleBtn     *widget.Button
	repeatBtn      *widget.Button
	art            *artTile // in the player bar
	bigArt         *artTile // on the Now Playing page
	artData        []byte   // cover of the playing track, or nil
	paused         bool
	unmuteLevel    float64 // volume to restore when unmuting
	vis            *visualizer
	visView        *fyne.Container
	visTitle       *widget.RichText
	visDetail      *widget.Label
	visModes       *widget.RadioGroup
	visBtn         *widget.Button
	visItem        *fyne.MenuItem
	visModeItems   []*fyne.MenuItem
	miniVis        *visualizer
	miniView       *fyne.Container
	miniItem       *fyne.MenuItem
	shuffleItem    *fyne.MenuItem
	speed          float64          // playback speed, 1 being normal (see speed.go)
	speedBtn       *widget.Button   // in the player bar
	speedItems     []*fyne.MenuItem // one per speed, in the Controls menu
	crossfeed      audio.CrossfeedLevel
	fadeInItem     *fyne.MenuItem
	eq             audio.EQSettings
	eqView         *eqView          // the Equalizer window, while open
	crossfeedItems []*fyne.MenuItem // one per level, in the Controls menu

	playlists       []*library.Playlist
	playlistsPath   string            // where they are saved; "" for nowhere
	playlist        *library.Playlist // the playlist shown in the song list, or nil
	playlistList    *fyne.Container
	playlistRows    map[*library.Playlist]*folderRow // each playlist's row in the sidebar
	newPlaylistBtn  *widget.Button
	rulesDialog     *rulesEditor     // the smart playlist dialog last opened, for tests
	appearanceItems []*fyne.MenuItem // View > Appearance (see appearance.go)
	repeatAllItem   *fyne.MenuItem
	repeatOneItem   *fyne.MenuItem
	seeking         bool // user is dragging the seek bar
	settingSeek     bool // the seek bar is being moved by code, not the user
	shuffle         bool
	repeat          repeatMode
	queue           []string        // shuffle: paths still to play this round
	queueValid      bool            // false means queue must be rebuilt before use
	history         []string        // paths played before the current one, for "previous"
	queued          []library.Track // Up Next: chosen to play before the list carries on (see upnext.go)
	fromQueue       bool            // the playing track came from Up Next
	listPath        string          // the last track played from the list, where it carries on from
	queueBtn        *widget.Button
	queuePop        *widget.PopUp
	cued            *cuedSong       // the last session's song, waiting for Play (see resume.go)
	selected        map[string]bool // paths of the songs selected (see selection.go)
	shownCols       []int           // the columns shown, in order (see columns.go)
	columnItems     []*fyne.MenuItem
	anchor          string // the song last ⌘- or ⇧-clicked, where ⇧-click selects from
	resumePending   bool   // the last session is still to be restored
	sessionSaved    bool   // saved on quit already
	clearQueueItem  *fyne.MenuItem

	// The fields below are only read or modified on the UI thread (inside
	// fyne.Do or widget callbacks), so they need no mutex.
	all      []library.Track // everything scanned (see songlist.go)
	tracks   []library.Track // what the table shows: all, filtered and sorted
	sortCol  int             // column the list is sorted by, or -1 for library order
	stats    string          // library summary shown under the title
	statsFor string          // the folder and number of tracks stats was worked out for
	sortDesc bool
	cancel   context.CancelFunc // stops the running scan; nil if none

	// The library's folders (see folders.go).
	folders      []*musicFolder
	folderStates atomic.Pointer[[]folderState] // for watchConnections
	filter       string                        // the folder the list is narrowed to, or "" for all
	scanning     string                        // the folder being scanned
	scanQueue    []string                      // folders waiting to be scanned
	discardScan  bool                          // the folder being scanned was removed or unplugged
	run          scanRun

	levels         map[string]quality.Level // saved verdicts, for the Quality column
	batch          *batchCheck              // the latest Check All Songs run (see batch.go)
	checkAllBtn    *widget.Button
	checkAllItem   *fyne.MenuItem
	batchBar       *fyne.Container // progress, or the summary when done
	batchProgress  *widget.ProgressBar
	batchLabel     *widget.Label
	batchStop      *widget.Button
	watchFolder    watchFunc // starts a watcher; tests use a fake
	settle         time.Duration
	reconnectEvery time.Duration // 0 turns off noticing a disconnected disk
}

// uiGoroutine is the goroutine that runs the UI: main's. It is 0 in tests,
// whose driver runs everything on the test's goroutine.
var uiGoroutine atomic.Uint64

// onUI runs f on the UI thread, as fyne.Do does, except while the app shuts
// down: Fyne then runs what it is handed at once, on the goroutine handing
// it over (a timer's, say), and the closing window must not be touched from
// there.
func onUI(f func()) {
	fyne.Do(func() {
		if id := uiGoroutine.Load(); id != 0 && goroutineID() != id {
			return
		}
		f()
	})
}

// goroutineID reads the current goroutine's number from its stack trace,
// which starts "goroutine 1 [running]:". Fyne tells the UI goroutine apart
// the same way.
func goroutineID() uint64 {
	var buf [32]byte
	s := strings.TrimPrefix(string(buf[:runtime.Stack(buf[:], false)]), "goroutine ")
	id, _ := strconv.ParseUint(s[:max(strings.IndexByte(s, ' '), 0)], 10, 64)
	return id
}

// Run opens Mugig's window and returns when it is closed. icon is the
// app's icon, a PNG with rounded corners.
func Run(icon []byte) {
	setIcon(icon)
	uiGoroutine.Store(goroutineID())
	quietFLACNotes()
	a := app.NewWithID(appID)
	a.Settings().SetTheme(appTheme{})
	a.SetIcon(appIcon)
	setDockIcon(iconPNG)
	w := a.NewWindow("Mugig")
	w.SetMaster() // closing it quits, even with the Equalizer window open

	media := newSystemMedia()
	pl := audio.NewPlayer(onUI)
	u := newUI(w, a.Preferences(), uiDeps{
		player:    pl,
		cache:     library.LoadCache(filepath.Join(a.Storage().RootURI().Path(), "library.gob")),
		saved:     quality.LoadStore(filepath.Join(a.Storage().RootURI().Path(), "quality.gob")),
		media:     media,
		playlists: filepath.Join(a.Storage().RootURI().Path(), "playlists.json"),
	})
	media.Start(u.remote)
	audio.WatchDevices(func() { onUI(u.devicesChanged) })
	go u.watchConnections()

	// Pick up where the user left off: the library, then the song.
	a.Lifecycle().SetOnStarted(func() {
		if len(u.folders) > 0 {
			u.scanLibrary()
		}
		if u.cancel == nil { // nothing to scan
			u.restoreSession()
		}
	})
	w.SetOnDropped(u.dropped)

	// Quitting from the menu or with ⌘Q stops the app without always
	// closing the window first, so the session is saved on whichever
	// comes first, while the player still knows the position.
	saveOnce := func() {
		if !u.sessionSaved {
			u.sessionSaved = true
			u.saveSession()
		}
	}
	a.Lifecycle().SetOnStopped(saveOnce)
	w.SetOnClosed(func() {
		saveOnce()
		u.stopAllWatching()
		if u.batch != nil && !u.batch.finished {
			u.batch.cancel()
			u.qualityStore.Save() // keep what was checked so far
		}
		if u.cancel != nil {
			u.cancel()
		}
		pl.Close() // also puts back the output device's sample rate
	})
	go u.trackProgress()
	w.Resize(fyne.NewSize(1200, 740))
	w.ShowAndRun()
}

// uiDeps are the parts of the UI that touch the outside world, so tests can
// replace them.
type uiDeps struct {
	player audioPlayer
	cache  *library.Cache
	saved  *quality.Store // optional; defaults to memory only
	media  mediaCenter    // optional
	async  func(func())   // optional; defaults to a new goroutine
	pick   picker         // optional; defaults to the system's Open panel
	// playlists is where playlists are saved; "" keeps them in memory.
	playlists string
}

func newUI(w fyne.Window, prefs fyne.Preferences, d uiDeps) *ui {
	u := &ui{win: w, prefs: prefs, player: d.player, cache: d.cache, qualityStore: d.saved, media: d.media, async: d.async}
	if u.qualityStore == nil {
		u.qualityStore = quality.LoadStore("")
	}
	u.levels = u.qualityStore.Levels()
	u.watchFolder, u.settle, u.reconnectEvery = library.WatchFolder, settleTime, reconnectPoll
	u.reveal, u.modifiers, u.trash = reveal, keyModifiers, trash.Move
	u.canHog = audio.CanHogDevice
	u.playlistsPath = d.playlists
	u.playlists = library.LoadPlaylists(d.playlists)
	u.selected = map[string]bool{}
	if u.pick = d.pick; u.pick == nil {
		u.pick = defaultPicker
	}
	u.save = defaultSaver
	if u.async == nil {
		u.async = func(f func()) { go f() }
	}
	u.build()
	return u
}

func (u *ui) build() {
	u.loadColumns()
	u.mainMenu = u.buildMenu()
	u.applyAppearance()
	u.win.SetMainMenu(u.mainMenu)
	u.win.Canvas().SetOnTypedKey(u.onTypedKey)

	u.status = secondaryLabel("")
	u.status.Truncation = fyne.TextTruncateOff
	u.status.Wrapping = fyne.TextWrapWord
	u.status.SizeName = theme.SizeNameCaptionText
	u.progress = widget.NewProgressBarInfinite()
	u.progress.Stop()
	u.progress.Hide()

	u.cancelBtn = widget.NewButtonWithIcon("Cancel Scan", theme.CancelIcon(), func() {
		if u.cancel != nil {
			u.cancel()
		}
	})
	u.cancelBtn.Hide()

	u.table = newTrackTable(
		func() (int, int) { return len(u.tracks), len(u.shownCols) },
		func() fyne.CanvasObject { return newTrackCell() },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			c := o.(*trackCell)
			col := u.colAt(id.Col)
			if id.Row < 0 || id.Row >= len(u.tracks) || col < 0 {
				c.set("", col, false)
				c.setSelected(false)
				c.onMenu, c.onTap = nil, nil
				return
			}
			t := u.tracks[id.Row]
			c.setSelected(u.selected[t.Path])
			c.onTap = func(at fyne.Position) { u.clickTrack(t, at) }
			if col == qualityCol {
				l := u.levelOf(t)
				c.setColoured(quality.LevelNames[l], levelColours[l])
			} else {
				c.set(cellText(t, col), col, t.Path == u.playing.Path)
			}
			c.onMenu = func(at fyne.Position) {
				widget.ShowPopUpMenuAtPosition(u.rowMenu(t), u.win.Canvas(), at)
			}
		},
		u.togglePlay, // Space
	)
	u.table.onEscape, u.table.onSelectAll = u.clearSelection, u.selectAll
	u.win.Canvas().AddShortcut(&fyne.ShortcutSelectAll{}, func(fyne.Shortcut) { u.selectAll() }) // nothing focused
	u.table.OnSelected = func(id widget.TableCellID) {
		// Skip the playing row so highlighting it (or clicking another
		// cell in it) doesn't restart the track.
		if id.Row >= 0 && id.Row < len(u.tracks) && u.tracks[id.Row].Path != u.playing.Path {
			u.jump(u.tracks[id.Row])
		}
		// Fyne highlights a single cell, not the row, which looks odd; the
		// playing row is marked by its icon and colour instead. Unselecting
		// also lets the same row be clicked again after a stop.
		u.table.UnselectAll()
	}
	u.sortCol = savedSortColumn(u.prefs)
	u.sortDesc = u.prefs.Bool(prefSortDesc)
	u.table.ShowHeaderRow = true
	u.table.CreateHeader = newHeader
	u.table.UpdateHeader = u.updateHeader
	u.applyColumns()

	u.search = widget.NewEntry()
	u.search.PlaceHolder = "Search title, artist or album"
	u.search.OnChanged = func(string) { u.applyView() }
	clear := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { u.search.SetText("") })
	clear.Importance = widget.LowImportance
	u.search.ActionItem = clear
	u.count = secondaryLabel("")

	pages := container.NewStack(u.buildSongsPage(), u.buildVisualizer()) // one shown at a time
	bar := u.buildTransport()
	u.loadFolders()
	u.resumePending = u.prefs.String(prefResumeSong) != "" || len(u.prefs.StringList(prefUpNext)) > 0
	sidebar := u.buildSidebar()
	u.updateCount()
	if len(u.folders) == 0 {
		u.status.SetText("Add a folder with your music to begin.")
	}
	u.showVisualizer(false)
	u.applyOutputPrefs()
	u.win.SetContent(container.NewBorder(nil, bar, sidebar, nil, pages))
}

// tapTable gives the table a click made at the absolute position at, as if
// it had been made on the table itself.
func (u *ui) tapTable(at fyne.Position) {
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(u.table)
	u.table.Tapped(&fyne.PointEvent{AbsolutePosition: at, Position: at.Subtract(origin)})
}

func cellText(t library.Track, col int) string {
	switch col {
	case titleCol:
		return t.Title
	case artistCol:
		return t.Artist
	case albumCol:
		return t.Album
	case timeCol:
		if t.Length > 0 {
			return humanize.Clock(t.Length)
		}
		return ""
	case yearCol:
		if t.Year > 0 {
			return strconv.Itoa(t.Year)
		}
		return ""
	case formatCol:
		return t.Format
	case sizeCol:
		return humanize.FileSize(t.Size)
	case pathCol:
		return t.Path
	}
	return "" // Quality: see levelOf
}

// queueScan scans library folders after any scans already running or
// waiting; at once if there are none.
func (u *ui) queueScan(roots ...string) {
	for _, root := range roots {
		if u.scanning != root && !slices.Contains(u.scanQueue, root) {
			u.scanQueue = append(u.scanQueue, root)
		}
	}
	if u.cancel == nil && len(u.scanQueue) > 0 {
		u.run = scanRun{started: time.Now()}
		u.nextScan()
	}
}

// nextScan starts the first waiting scan, if any.
func (u *ui) nextScan() {
	if len(u.scanQueue) == 0 {
		return
	}
	root := u.scanQueue[0]
	u.scanQueue = u.scanQueue[1:]
	u.startScan(root)
}

// scanRun totals the scans started together, for the status line.
type scanRun struct {
	started                            time.Time
	folders, tracks, fromCache, noTags int
}

// startScan is called on the UI thread. It sets up the UI for a scan, then
// hands the work to runScan in the background. The folder's songs stay
// listed while it is scanned again.
func (u *ui) startScan(root string) {
	u.rescanBtn.Disable()
	u.cancelBtn.Show()
	u.progress.Show()
	u.progress.Start()
	u.status.SetText(fmt.Sprintf("Scanning %s…", filepath.Base(root)))

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel, u.scanning = cancel, root
	if f := u.folderAt(root); f != nil {
		u.startWatching(f) // changes during the scan are applied when it ends
	}
	u.refreshFolders()
	u.async(func() { u.runScan(ctx, root, library.ScanWorkers(root)) })
}

// runScan walks root with a pool of workers, reporting to the UI through
// flush and finish. It runs off the UI thread and returns when done.
func (u *ui) runScan(ctx context.Context, root string, workers int) {
	paths := make(chan string, 1024)
	results := make(chan library.ScanResult, 1024)

	go library.Walk(ctx, root, paths)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go library.Worker(ctx, u.cache, paths, results, &wg)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	u.collect(ctx, root, results)
}

// scanTally is what one scan found.
type scanTally struct {
	found     map[string]bool // every track's path
	fromCache int
	noTags    int
	cancelled bool
	complete  bool // not cancelled, and the folder was there throughout
}

// collect gathers results off the UI thread and pushes them to the GUI in
// batches, so the interface stays responsive even with many files. At the
// end it updates and saves the cache; the workers have stopped by then, so
// nothing else is using it.
func (u *ui) collect(ctx context.Context, root string, results <-chan library.ScanResult) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	seen := map[string]library.CacheEntry{}
	tally := scanTally{found: map[string]bool{}}
	var batch []library.Track
	for {
		select {
		case r, ok := <-results:
			if !ok {
				u.flush(root, batch)
				// A disk unplugged mid-scan reads as empty folders, which
				// mustn't be taken for deleted songs.
				_, err := os.Stat(root)
				tally.cancelled = ctx.Err() != nil
				tally.complete = !tally.cancelled && err == nil
				u.cache.Update(root, seen, tally.complete)
				if err := u.cache.Save(); err != nil {
					fyne.LogError("saving library cache", err)
				}
				u.finish(root, tally)
				return
			}
			if r.ModTime != 0 {
				seen[r.Path] = r.Entry()
			}
			if r.Cached {
				tally.fromCache++
			}
			if r.Track.NoTags {
				tally.noTags++
			}
			for _, t := range r.Songs() {
				tally.found[t.Path] = true
				batch = append(batch, t)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				u.flush(root, batch)
				batch = nil // hand ownership of the old slice to the UI
			}
		}
	}
}

// flush adds a batch of scanned tracks to the list, or updates them if
// listed already.
func (u *ui) flush(root string, batch []library.Track) {
	if len(batch) == 0 {
		return
	}
	onUI(func() {
		if u.discardScan && u.scanning == root {
			return // the folder was removed or unplugged
		}
		index := make(map[string]int, len(u.all))
		for i, t := range u.all {
			index[t.Path] = i
		}
		// Append new tracks unsorted; the view is sorted once at the end.
		words := u.searchWords()
		for _, t := range batch {
			if i, ok := index[t.Path]; ok {
				u.all[i] = t
				continue
			}
			u.all = append(u.all, t)
			if u.inFilter(t) && library.Matches(t, words) {
				u.tracks = append(u.tracks, t)
			}
		}
		if images := library.CueImages(u.all); len(images) > 0 {
			u.all, u.tracks = library.WithoutImages(u.all, images), library.WithoutImages(u.tracks, images)
		}
		u.table.Refresh()
		u.updateCount()
		u.status.SetText(fmt.Sprintf("Scanning %s… %s found", filepath.Base(root), humanize.Plural(u.countIn(root), "track")))
	})
}

func (u *ui) finish(root string, tally scanTally) {
	onUI(func() {
		// A folder removed or unplugged during its scan: what was found
		// is dropped, and other folders' scans go on.
		discard := u.discardScan && u.scanning == root
		cancelled := tally.cancelled && !discard // by the user: the rest is cancelled too
		u.cancel, u.scanning, u.discardScan = nil, "", false
		switch {
		case discard:
			u.dropTracksIn(root)
			if u.folderAt(root) == nil { // removed: what the scan cached is unwanted
				cache := u.cache
				u.async(func() {
					cache.ForgetUnder(root)
					if err := cache.Save(); err != nil {
						fyne.LogError("saving library cache", err)
					}
				})
			}
		case tally.complete:
			// Songs no longer there are taken off the list.
			u.all = slices.DeleteFunc(u.all, func(t library.Track) bool { return library.Within(t.Path, root) && !tally.found[t.Path] })
		}
		u.all = library.DropImages(u.all)
		// Sort once at the end rather than on every batch.
		sort.SliceStable(u.all, func(i, j int) bool {
			return library.Less(u.all[i], u.all[j])
		})
		u.applyView() // also redraws the playing marker, which may have moved

		if !discard {
			u.run.folders++
			u.run.tracks += len(tally.found)
			u.run.fromCache += tally.fromCache
			u.run.noTags += tally.noTags
		}
		if cancelled {
			u.scanQueue = nil
		}
		if len(u.scanQueue) > 0 {
			u.nextScan()
			return
		}

		u.progress.Stop()
		u.progress.Hide()
		u.cancelBtn.Hide()
		u.rescanBtn.Enable()
		u.refreshFolders()
		for _, f := range u.folders {
			if f.watch != nil {
				defer u.applyPending(f.watch) // changes that came during the scan
			}
		}

		verb := "Scanned"
		if cancelled {
			verb = "Scan cancelled:"
		}
		in := ""
		if u.run.folders > 1 {
			in = " in " + humanize.Plural(u.run.folders, "folder")
		}
		elapsed := time.Duration(0)
		if !u.run.started.IsZero() {
			elapsed = time.Since(u.run.started)
		}
		u.status.SetText(fmt.Sprintf("%s %s%s in %s\n%s unchanged · %s without tags",
			verb, humanize.Plural(u.run.tracks, "track"), in, elapsed.Round(10*time.Millisecond),
			humanize.Thousands(u.run.fromCache), humanize.Thousands(u.run.noTags)))
		u.run = scanRun{}
		u.restoreSession()
	})
}

// countIn counts the listed tracks in dir.
func (u *ui) countIn(dir string) int {
	n := 0
	for _, t := range u.all {
		if library.Within(t.Path, dir) {
			n++
		}
	}
	return n
}
