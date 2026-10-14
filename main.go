// Mugig: scans a folder for audio files, reads their tags with a
// worker pool, and shows the results in a Fyne table with batched updates.
// Clicking a row plays that track; the bar at the bottom controls playback.
//
// Requires Fyne v2.6+ (for fyne.Do).
package main

import (
	"context"
	"fmt"
	"io/fs"
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

	"github.com/dhowden/tag"
	"mugig/internal/trash"
)

// Track holds what we report to the GUI for one audio file.
type Track struct {
	Path   string
	Title  string
	Artist string
	Album  string
	Year   int
	Format string
	Size   int64         // bytes
	Length time.Duration // 0 if unknown
	NoTags bool          // true if tags were missing or unreadable
	search string        // lower-cased title, artist and album for the search box
}

// audioExts maps lower-case extensions to a display format name.
var audioExts = map[string]string{
	".mp3":  "MP3",
	".flac": "FLAC",
	".ogg":  "OGG",
	".wav":  "WAV", // usually has no tags, so the file name is the title
}

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

// walk finds audio files under root and sends their paths to the channel.
// It closes paths when finished or cancelled.
func walk(ctx context.Context, root string, paths chan<- string) {
	defer close(paths)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err() // stop walking on cancel
		}
		if err != nil {
			return nil // unreadable file or folder: skip it, keep going
		}
		if d.IsDir() {
			if p != root && hidden(p) {
				return filepath.SkipDir // e.g. .Trashes, which holds deleted songs
			}
			return nil
		}
		if !isAudioFile(p) || isEmpty(d) {
			return nil
		}
		select {
		case paths <- p:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// scanResult is one scanned file, with what the cache needs to know.
type scanResult struct {
	track   Track
	size    int64
	modTime int64 // 0 if the file couldn't be checked; not cached then
	cached  bool  // tags came from the cache, not the file
}

// worker reads tags for each path, unless the cache has them, and sends the
// result.
func worker(ctx context.Context, cache *libraryCache, paths <-chan string, results chan<- scanResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for p := range paths {
		if ctx.Err() != nil {
			return
		}
		select {
		case results <- scanFile(cache, p):
		case <-ctx.Done():
			return
		}
	}
}

func scanFile(cache *libraryCache, path string) scanResult {
	var r scanResult
	if fi, err := os.Stat(path); err == nil {
		r.size, r.modTime = fi.Size(), fi.ModTime().UnixNano()
		r.track, r.cached = cache.lookup(path, r.size, r.modTime)
	}
	if !r.cached {
		r.track = readTrack(path)
	} else if r.track.Length == 0 {
		// Cached before lengths were read, or the disk didn't answer then.
		r.track.Length = fileLength(path)
	}
	r.track.Size = r.size // also for tracks cached before sizes were shown
	r.track.search = searchKey(r.track)
	return r
}

// readTrack opens one file and parses its tags. Files with missing or broken
// tags fall back to using the file name as the title.
// isAudioFile reports whether path names a file the app can play. Hidden
// files are left out: macOS puts a "._" file beside every file it copies
// to an exFAT or FAT disk, such as most USB drives, and "._song.flac" holds
// metadata, not audio.
func isAudioFile(path string) bool {
	return !hidden(path) && audioExts[strings.ToLower(filepath.Ext(path))] != ""
}

// isEmpty reports whether a file has no data, as left by an interrupted
// download or copy. There is nothing to play.
func isEmpty(d fs.DirEntry) bool {
	fi, err := d.Info()
	return err == nil && fi.Size() == 0
}

// hidden reports whether path's name starts with a dot, or is __MACOSX: the
// folder macOS adds to zip files, whose copies of the files hold metadata,
// not audio.
func hidden(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, ".") || name == "__MACOSX"
}

func readTrack(path string) Track {
	t := Track{
		Path:   path,
		Format: audioExts[strings.ToLower(filepath.Ext(path))],
	}
	fallbackTitle := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	f, err := os.Open(path)
	if err != nil {
		t.Title, t.NoTags = fallbackTitle, true
		return t
	}
	defer f.Close()
	t.Length = trackLength(f)

	m, err := tag.ReadFrom(f) // reads only the tag section, not the audio
	if err != nil {
		t.Title, t.NoTags = fallbackTitle, true
		return t
	}

	t.Title = strings.TrimSpace(m.Title())
	t.Artist = strings.TrimSpace(m.Artist())
	t.Album = strings.TrimSpace(m.Album())
	t.Year = m.Year()
	if t.Title == "" {
		t.Title = fallbackTitle
	}
	return t
}

// ---------------------------------------------------------------------------
// GUI
// ---------------------------------------------------------------------------

type ui struct {
	win       fyne.Window
	prefs     fyne.Preferences
	cache     *libraryCache
	media     mediaCenter
	async     func(func())            // runs slow work off the UI thread
	pick      picker                  // shows an Open panel (see open.go)
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
	player          audioPlayer
	playing         Track // zero when stopped
	prevBtn         *widget.Button
	playBtn         *roundButton
	nextBtn         *widget.Button
	nowTitle        *widget.Label
	nowArtist       *widget.Label
	seek            *widget.Slider
	elapsed         *widget.Label
	total           *widget.Label
	timeSlots       [2]*canvas.Rectangle // keep the time labels a steady width
	barBadge        *qualityBadge        // lossless or not, in the player bar
	np              *qualityPanel        // the playing track's quality, on the Now Playing page
	barArtistRow    *fyne.Container      // barBadge and the artist
	npQuality       *fyne.Container      // npChecks and checkBtn
	npOutput        *widget.Label        // where the sound goes, on the Now Playing page
	outputItem      *fyne.MenuItem       // the Output Device submenu
	matchRateItem   *fyne.MenuItem
	checkBtn        *widget.Button // Check Audio Quality, on the Now Playing page
	checkItem       *fyne.MenuItem // the same in the menu
	quality         trackQuality   // the playing track's (see qualityview.go)
	qualityStore    *qualityStore
	volume          *widget.Slider
	muteBtn         *widget.Button
	shuffleBtn      *widget.Button
	repeatBtn       *widget.Button
	art             *artTile // in the player bar
	bigArt          *artTile // on the Now Playing page
	artData         []byte   // cover of the playing track, or nil
	paused          bool
	unmuteLevel     float64 // volume to restore when unmuting
	vis             *visualizer
	visView         *fyne.Container
	visTitle        *widget.RichText
	visDetail       *widget.Label
	visModes        *widget.RadioGroup
	visBtn          *widget.Button
	visItem         *fyne.MenuItem
	visModeItems    []*fyne.MenuItem
	miniVis         *visualizer
	miniView        *fyne.Container
	miniItem        *fyne.MenuItem
	shuffleItem     *fyne.MenuItem
	speed           float64          // playback speed, 1 being normal (see speed.go)
	speedBtn        *widget.Button   // in the player bar
	speedItems      []*fyne.MenuItem // one per speed, in the Controls menu
	appearanceItems []*fyne.MenuItem // View > Appearance (see appearance.go)
	repeatAllItem   *fyne.MenuItem
	repeatOneItem   *fyne.MenuItem
	seeking         bool // user is dragging the seek bar
	settingSeek     bool // the seek bar is being moved by code, not the user
	shuffle         bool
	repeat          repeatMode
	queue           []string // shuffle: paths still to play this round
	queueValid      bool     // false means queue must be rebuilt before use
	history         []string // paths played before the current one, for "previous"
	queued          []Track  // Up Next: chosen to play before the list carries on (see upnext.go)
	fromQueue       bool     // the playing track came from Up Next
	listPath        string   // the last track played from the list, where it carries on from
	queueBtn        *widget.Button
	queuePop        *widget.PopUp
	cued            *cuedSong       // the last session's song, waiting for Play (see resume.go)
	selected        map[string]bool // paths of the songs selected (see selection.go)
	anchor          string          // the song last ⌘- or ⇧-clicked, where ⇧-click selects from
	resumePending   bool            // the last session is still to be restored
	sessionSaved    bool            // saved on quit already
	clearQueueItem  *fyne.MenuItem

	// The fields below are only read or modified on the UI thread (inside
	// fyne.Do or widget callbacks), so they need no mutex.
	all      []Track // everything scanned (see library.go)
	tracks   []Track // what the table shows: all, filtered and sorted
	sortCol  int     // column the list is sorted by, or -1 for library order
	stats    string  // library summary shown under the title
	statsFor string  // the folder and number of tracks stats was worked out for
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

	levels         map[string]qualityLevel // saved verdicts, for the Quality column
	batch          *batchCheck             // the latest Check All Songs run (see batch.go)
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

func main() {
	uiGoroutine.Store(goroutineID())
	quietFLACNotes()
	a := app.NewWithID(appID)
	a.Settings().SetTheme(appTheme{})
	a.SetIcon(appIcon)
	setDockIcon(iconPNG)
	w := a.NewWindow("Mugig")

	media := newSystemMedia()
	audio := newPlayer()
	u := newUI(w, a.Preferences(), uiDeps{
		player: audio,
		cache:  loadCache(filepath.Join(a.Storage().RootURI().Path(), "library.gob")),
		saved:  loadQualityStore(filepath.Join(a.Storage().RootURI().Path(), "quality.gob")),
		media:  media,
	})
	media.Start(u.remote)
	watchDevices(u.devicesChanged)
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
			u.qualityStore.save() // keep what was checked so far
		}
		if u.cancel != nil {
			u.cancel()
		}
		audio.Close() // also puts back the output device's sample rate
	})
	go u.trackProgress()
	w.Resize(fyne.NewSize(1200, 740))
	w.ShowAndRun()
}

// uiDeps are the parts of the UI that touch the outside world, so tests can
// replace them.
type uiDeps struct {
	player audioPlayer
	cache  *libraryCache
	saved  *qualityStore // optional; defaults to memory only
	media  mediaCenter   // optional
	async  func(func())  // optional; defaults to a new goroutine
	pick   picker        // optional; defaults to the system's Open panel
}

func newUI(w fyne.Window, prefs fyne.Preferences, d uiDeps) *ui {
	u := &ui{win: w, prefs: prefs, player: d.player, cache: d.cache, qualityStore: d.saved, media: d.media, async: d.async}
	if u.qualityStore == nil {
		u.qualityStore = loadQualityStore("")
	}
	u.levels = u.qualityStore.levels()
	u.watchFolder, u.settle, u.reconnectEvery = watchFolder, settleTime, reconnectPoll
	u.reveal, u.modifiers, u.trash = reveal, keyModifiers, trash.Move
	u.selected = map[string]bool{}
	if u.pick = d.pick; u.pick == nil {
		u.pick = defaultPicker
	}
	if u.async == nil {
		u.async = func(f func()) { go f() }
	}
	u.build()
	return u
}

func (u *ui) build() {
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
		func() (int, int) { return len(u.tracks), len(columns) },
		func() fyne.CanvasObject { return newTrackCell() },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			c := o.(*trackCell)
			if id.Row < 0 || id.Row >= len(u.tracks) {
				c.set("", id.Col, false)
				c.setSelected(false)
				c.onMenu, c.onTap = nil, nil
				return
			}
			t := u.tracks[id.Row]
			c.setSelected(u.selected[t.Path])
			c.onTap = func(at fyne.Position) { u.clickTrack(t, at) }
			if id.Col == qualityCol {
				l := u.levelOf(t)
				c.setColoured(levelNames[l], levelColours[l])
			} else {
				c.set(cellText(t, id.Col), id.Col, t.Path == u.playing.Path)
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
	for i, width := range []float32{250, 170, 190, 70, 70, 80, 85, 100, 400} {
		u.table.SetColumnWidth(i, width)
	}

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

func cellText(t Track, col int) string {
	switch col {
	case titleCol:
		return t.Title
	case artistCol:
		return t.Artist
	case albumCol:
		return t.Album
	case timeCol:
		if t.Length > 0 {
			return formatTime(t.Length)
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
		return fileSize(t.Size)
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
	u.async(func() { u.runScan(ctx, root, scanWorkers(root)) })
}

// runScan walks root with a pool of workers, reporting to the UI through
// flush and finish. It runs off the UI thread and returns when done.
func (u *ui) runScan(ctx context.Context, root string, workers int) {
	paths := make(chan string, 1024)
	results := make(chan scanResult, 1024)

	go walk(ctx, root, paths)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go worker(ctx, u.cache, paths, results, &wg)
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
func (u *ui) collect(ctx context.Context, root string, results <-chan scanResult) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	seen := map[string]cacheEntry{}
	tally := scanTally{found: map[string]bool{}}
	var batch []Track
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
				u.cache.update(root, seen, tally.complete)
				if err := u.cache.save(); err != nil {
					fyne.LogError("saving library cache", err)
				}
				u.finish(root, tally)
				return
			}
			if r.modTime != 0 {
				seen[r.track.Path] = cacheEntry{Size: r.size, ModTime: r.modTime, Track: r.track}
			}
			if r.cached {
				tally.fromCache++
			}
			if r.track.NoTags {
				tally.noTags++
			}
			tally.found[r.track.Path] = true
			batch = append(batch, r.track)
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
func (u *ui) flush(root string, batch []Track) {
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
			if u.inFilter(t) && matches(t, words) {
				u.tracks = append(u.tracks, t)
			}
		}
		u.table.Refresh()
		u.updateCount()
		u.status.SetText(fmt.Sprintf("Scanning %s… %s found", filepath.Base(root), plural(u.countIn(root), "track")))
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
					cache.forgetUnder(root)
					if err := cache.save(); err != nil {
						fyne.LogError("saving library cache", err)
					}
				})
			}
		case tally.complete:
			// Songs no longer there are taken off the list.
			u.all = slices.DeleteFunc(u.all, func(t Track) bool { return within(t.Path, root) && !tally.found[t.Path] })
		}
		// Sort once at the end rather than on every batch.
		sort.SliceStable(u.all, func(i, j int) bool {
			return libraryLess(u.all[i], u.all[j])
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
			in = " in " + plural(u.run.folders, "folder")
		}
		elapsed := time.Duration(0)
		if !u.run.started.IsZero() {
			elapsed = time.Since(u.run.started)
		}
		u.status.SetText(fmt.Sprintf("%s %s%s in %s\n%s unchanged · %s without tags",
			verb, plural(u.run.tracks, "track"), in, elapsed.Round(10*time.Millisecond),
			thousands(u.run.fromCache), thousands(u.run.noTags)))
		u.run = scanRun{}
		u.restoreSession()
	})
}

// countIn counts the listed tracks in dir.
func (u *ui) countIn(dir string) int {
	n := 0
	for _, t := range u.all {
		if within(t.Path, dir) {
			n++
		}
	}
	return n
}
