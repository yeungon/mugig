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
)

// Track holds what we report to the GUI for one audio file.
type Track struct {
	Path   string
	Title  string
	Artist string
	Album  string
	Year   int
	Format string
	NoTags bool   // true if tags were missing or unreadable
	search string // lower-cased title, artist and album for the search box
}

// audioExts maps lower-case extensions to a display format name.
var audioExts = map[string]string{
	".mp3":  "MP3",
	".flac": "FLAC",
	".ogg":  "OGG",
	".wav":  "WAV", // usually has no tags, so the file name is the title
}

var columns = []string{"Title", "Artist", "Album", "Year", "Format", "Quality", "Path"}

// appID identifies the app to the OS; preferences are stored under it, so
// changing it forgets saved settings. It keeps the app's original name,
// Music Scanner, for that reason.
const appID = "org.kiwipanel.musicscan"

// Preference keys for settings kept between launches.
const (
	prefFolder   = "lastFolder"
	prefVolume   = "volume"
	prefShuffle  = "shuffle"
	prefRepeat   = "repeat"
	prefSortCol  = "sortColumn"
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
	}
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
	async     func(func()) // runs slow work off the UI thread
	pick      picker       // shows an Open panel (see open.go)
	mainMenu  *fyne.MainMenu
	table     *trackTable
	search    *widget.Entry
	count     *widget.Label
	status    *widget.Label
	progress  *widget.ProgressBarInfinite
	chooseBtn *widget.Button
	rescanBtn *widget.Button
	cancelBtn *widget.Button

	// Layout (see layout.go).
	songsPage     *fyne.Container
	navSongs      *widget.Button
	navNowPlaying *widget.Button
	folderName    *widget.Label
	folderPath    *widget.Label

	// Playback (see transport.go).
	player        audioPlayer
	playing       Track // zero when stopped
	prevBtn       *widget.Button
	playBtn       *roundButton
	nextBtn       *widget.Button
	nowTitle      *widget.Label
	nowArtist     *widget.Label
	seek          *widget.Slider
	elapsed       *widget.Label
	total         *widget.Label
	timeSlots     [2]*canvas.Rectangle // keep the time labels a steady width
	barBadge      *qualityBadge        // lossless or not, in the player bar
	np            *qualityPanel        // the playing track's quality, on the Now Playing page
	barArtistRow  *fyne.Container      // barBadge and the artist
	npQuality     *fyne.Container      // npChecks and checkBtn
	npOutput      *widget.Label        // where the sound goes, on the Now Playing page
	outputItem    *fyne.MenuItem       // the Output Device submenu
	matchRateItem *fyne.MenuItem
	checkBtn      *widget.Button // Check Audio Quality, on the Now Playing page
	checkItem     *fyne.MenuItem // the same in the menu
	quality       trackQuality   // the playing track's (see qualityview.go)
	qualityStore  *qualityStore
	volume        *widget.Slider
	muteBtn       *widget.Button
	shuffleBtn    *widget.Button
	repeatBtn     *widget.Button
	art           *artTile // in the player bar
	bigArt        *artTile // on the Now Playing page
	artData       []byte   // cover of the playing track, or nil
	paused        bool
	unmuteLevel   float64 // volume to restore when unmuting
	vis           *visualizer
	visView       *fyne.Container
	visTitle      *widget.RichText
	visDetail     *widget.Label
	visModes      *widget.RadioGroup
	visBtn        *widget.Button
	visItem       *fyne.MenuItem
	visModeItems  []*fyne.MenuItem
	miniVis       *visualizer
	miniView      *fyne.Container
	miniItem      *fyne.MenuItem
	shuffleItem   *fyne.MenuItem
	speed         float64          // playback speed, 1 being normal (see speed.go)
	speedBtn      *widget.Button   // in the player bar
	speedItems    []*fyne.MenuItem // one per speed, in the Controls menu
	repeatAllItem *fyne.MenuItem
	repeatOneItem *fyne.MenuItem
	seeking       bool // user is dragging the seek bar
	settingSeek   bool // the seek bar is being moved by code, not the user
	shuffle       bool
	repeat        repeatMode
	queue         []string // shuffle: paths still to play this round
	queueValid    bool     // false means queue must be rebuilt before use
	history       []string // paths played before the current one, for "previous"

	// The fields below are only read or modified on the UI thread (inside
	// fyne.Do or widget callbacks), so they need no mutex.
	all      []Track // everything scanned (see library.go)
	tracks   []Track // what the table shows: all, filtered and sorted
	sortCol  int     // column the list is sorted by, or -1 for library order
	stats    string  // library summary shown under the title
	statsFor int     // library size stats was worked out for
	sortDesc bool
	cancel   context.CancelFunc

	levels         map[string]qualityLevel // saved verdicts, for the Quality column
	batch          *batchCheck             // the latest Check All Songs run (see batch.go)
	checkAllBtn    *widget.Button
	checkAllItem   *fyne.MenuItem
	batchBar       *fyne.Container // progress, or the summary when done
	batchProgress  *widget.ProgressBar
	batchLabel     *widget.Label
	batchStop      *widget.Button
	watch          *folderWatch // follows the music folder (see watch.go)
	watchBusy      bool         // changes are being read
	watchFolder    watchFunc    // starts a watcher; tests use a fake
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

	// Pick up where the user left off.
	a.Lifecycle().SetOnStarted(func() {
		if dir := u.lastFolder(); dir != "" {
			u.openFolder(dir, false)
		} else if missing := u.prefs.String(prefFolder); missing != "" {
			u.status.SetText(fmt.Sprintf("Can't find %s. If it's on a removable disk, connect it and choose Rescan.", missing))
		}
	})
	w.SetOnDropped(u.dropped)

	w.SetOnClosed(func() {
		u.stopWatching()
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
	u.win.SetMainMenu(u.mainMenu)
	u.win.Canvas().SetOnTypedKey(u.onTypedKey)

	u.status = secondaryLabel("")
	u.status.Truncation = fyne.TextTruncateOff
	u.status.Wrapping = fyne.TextWrapWord
	u.status.SizeName = theme.SizeNameCaptionText
	u.progress = widget.NewProgressBarInfinite()
	u.progress.Stop()
	u.progress.Hide()

	u.chooseBtn = widget.NewButtonWithIcon("Choose Folder…", theme.FolderOpenIcon(), u.chooseFolder)
	u.cancelBtn = widget.NewButtonWithIcon("Cancel Scan", theme.CancelIcon(), func() {
		if u.cancel != nil {
			u.cancel()
		}
	})
	u.cancelBtn.Hide()

	u.table = newTrackTable(
		func() (int, int) { return len(u.tracks), len(columns) },
		func() fyne.CanvasObject { return newTrackCell(u.tapTable) },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			c := o.(*trackCell)
			if id.Row < 0 || id.Row >= len(u.tracks) {
				c.set("", id.Col, false)
				c.onMenu = nil
				return
			}
			t := u.tracks[id.Row]
			if id.Col == qualityCol {
				l := u.levelOf(t)
				c.setColoured(levelNames[l], levelColours[l])
			} else {
				c.set(cellText(t, id.Col), id.Col, t.Path == u.playing.Path)
			}
			c.onMenu = func(at fyne.Position) {
				widget.ShowPopUpMenuAtPosition(u.trackMenu(t), u.win.Canvas(), at)
			}
		},
		u.togglePlay, // Space
	)
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
	u.sortCol = u.prefs.IntWithFallback(prefSortCol, -1)
	u.sortDesc = u.prefs.Bool(prefSortDesc)
	u.table.ShowHeaderRow = true
	u.table.CreateHeader = newHeader
	u.table.UpdateHeader = u.updateHeader
	for i, width := range []float32{280, 200, 220, 70, 80, 100, 400} {
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
	sidebar := u.buildSidebar()
	u.updateCount()
	u.status.SetText("Choose a folder to scan.")
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
	case 0:
		return t.Title
	case 1:
		return t.Artist
	case 2:
		return t.Album
	case 3:
		if t.Year > 0 {
			return strconv.Itoa(t.Year)
		}
		return ""
	case 4:
		return t.Format
	case 6:
		return t.Path
	}
	return "" // Quality: see levelOf
}

// lastFolder returns the previously scanned folder, or "" if there is none
// or it no longer exists (say, an unplugged drive).
func (u *ui) lastFolder() string {
	dir := u.prefs.String(prefFolder)
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}

// rescan scans the last folder again, or asks for one if there is none.
func (u *ui) rescan() {
	if u.cancel != nil {
		return // already scanning
	}
	if dir := u.lastFolder(); dir != "" {
		u.openFolder(dir, true)
	} else {
		u.chooseFolder()
	}
}

// startScan is called on the UI thread. It sets up the UI for a scan, then
// hands the work to runScan in the background.
func (u *ui) startScan(root string) {
	u.prefs.SetString(prefFolder, root)
	u.showFolder(root)
	u.table.UnselectAll() // row indexes are about to change
	u.all, u.tracks = nil, nil
	u.queueValid = false
	u.table.Refresh()
	u.table.ScrollToTop()
	u.updateCount()

	u.chooseBtn.Disable()
	u.rescanBtn.Disable()
	u.cancelBtn.Show()
	u.progress.Show()
	u.progress.Start()
	u.status.SetText("Scanning…")

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.startWatching(root) // changes during the scan are applied when it ends
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

	u.collect(ctx, root, results, time.Now())
}

// collect gathers results off the UI thread and pushes them to the GUI in
// batches, so the interface stays responsive even with many files. At the
// end it updates and saves the cache; the workers have stopped by then, so
// nothing else is using it.
func (u *ui) collect(ctx context.Context, root string, results <-chan scanResult, started time.Time) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	seen := map[string]cacheEntry{}
	fromCache := 0
	var batch []Track
	for {
		select {
		case r, ok := <-results:
			if !ok {
				u.flush(batch)
				u.cache.update(root, seen, ctx.Err() == nil)
				if err := u.cache.save(); err != nil {
					fyne.LogError("saving library cache", err)
				}
				u.finish(ctx.Err() != nil, time.Since(started), fromCache)
				return
			}
			if r.modTime != 0 {
				seen[r.track.Path] = cacheEntry{Size: r.size, ModTime: r.modTime, Track: r.track}
			}
			if r.cached {
				fromCache++
			}
			batch = append(batch, r.track)
		case <-ticker.C:
			if len(batch) > 0 {
				u.flush(batch)
				batch = nil // hand ownership of the old slice to the UI
			}
		}
	}
}

func (u *ui) flush(batch []Track) {
	if len(batch) == 0 {
		return
	}
	onUI(func() {
		u.all = append(u.all, batch...)
		// Append matching tracks unsorted; the view is sorted once at the end.
		words := u.searchWords()
		for _, t := range batch {
			if matches(t, words) {
				u.tracks = append(u.tracks, t)
			}
		}
		u.table.Refresh()
		u.updateCount()
		u.status.SetText(fmt.Sprintf("Scanning… %s found", plural(len(u.all), "track")))
	})
}

func (u *ui) finish(cancelled bool, elapsed time.Duration, fromCache int) {
	onUI(func() {
		// Sort once at the end rather than on every batch.
		sort.SliceStable(u.all, func(i, j int) bool {
			return libraryLess(u.all[i], u.all[j])
		})
		u.applyView() // also redraws the playing marker, which may have moved

		noTags := 0
		for _, t := range u.all {
			if t.NoTags {
				noTags++
			}
		}

		u.progress.Stop()
		u.progress.Hide()
		u.cancelBtn.Hide()
		u.chooseBtn.Enable()
		u.rescanBtn.Enable()
		u.cancel = nil
		if u.watch != nil {
			defer u.applyPending(u.watch) // changes that came during the scan
		}

		verb := "Scanned"
		if cancelled {
			verb = "Scan cancelled:"
		}
		u.status.SetText(fmt.Sprintf("%s %s in %s\n%s unchanged · %s without tags",
			verb, plural(len(u.all), "track"), elapsed.Round(10*time.Millisecond),
			thousands(fromCache), thousands(noTags)))
	})
}
