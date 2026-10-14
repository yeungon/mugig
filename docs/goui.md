# Go and Fyne: lessons from building Mugig

Notes for whoever works on Mugig next, or builds another desktop app in Go
with Fyne. Each item is something that cost time to find out. Versions at
the time: Go 1.24, Fyne v2.8.2, fyne tools v1.7.3, fyne-cross v1.6.3,
gopxl/beep v2, malgo (miniaudio), mewkiz/flac v1.0.14, on macOS (Apple
silicon).

---

## 1. Fyne: threads

- **Touch widgets only on the UI thread.** From any other goroutine, hand
  the work over with `fyne.Do`. Fyne logs "Error in Fyne call thread" when
  this is broken, and the next major release will stop forgiving it.
- **At shutdown, `fyne.Do` runs the function on the caller's goroutine.**
  Once Fyne's run loop has drained its queue (on ⌘Q, closing the window, or
  SIGINT/SIGTERM), anything handed to `fyne.Do` runs at once, on whatever
  goroutine called it — a ticker's, a worker's. It then touches widgets off
  the UI thread and logs the warning. `Window.SetOnClosed` is *not*
  reliably called before that point when quitting by ⌘Q or a signal, so a
  "closing" flag set there doesn't help. What works: note the main
  goroutine's ID at the start of `main()`, and skip the work when it runs
  anywhere else. See `onUI` and `goroutineID` in `main.go`; all UI updates
  from goroutines go through `onUI`. Fyne tells goroutines apart the same
  way internally.
- **Timers that redraw** (the visualizer, the position display) are where
  the shutdown problem shows first, as they run all the time.

## 2. Fyne: layout and widgets

- **Refresh the container after changing its contents.** Swapping
  `Objects`, showing or hiding children, or changing text that alters a
  size doesn't always lay the parent out again. Call `Refresh()` on the
  container (and on the dialog, then `Resize` it if it should grow).
- **A truncating label collapses to "…" in an `HBox`.** A label with
  `Truncation` set has almost no minimum width, and an `HBox` gives it
  exactly that. Use plain labels in boxes; keep truncation for labels whose
  width is set by the layout (borders, grids).
- **Buttons stretch to the row's height.** In an `HBox` next to taller
  things, a filled (`HighImportance`) button becomes a tall block. Wrap it
  in `container.NewCenter`.
- **Table selection is per cell, not per row.** `OnSelected` fires for a
  cell; to act on rows, act and then `UnselectAll()`, and mark the current
  row yourself. Clicking a cell inside a custom cell widget that handles
  taps needs forwarding to the table (`tapTable`).
- **Modifier keys on click**: `Tapped` doesn't say whether ⌘ or ⇧ was
  held. Ask the driver: `fyne.CurrentApp().Driver().(desktop.Driver)
  .CurrentKeyModifiers()` (Fyne 2.4+). The test driver doesn't have it, so
  make it replaceable in tests.
- **Don't make ⌘A a menu shortcut** in an app with a search box: menu
  shortcuts fire wherever focus is, so it would stop ⌘A selecting the
  search text. Handle `fyne.ShortcutSelectAll` in the list's
  `TypedShortcut`, and on the canvas for when nothing has focus.
- **Hiding table columns**: a column of width 0 still leaves a gap. Map
  the table's column positions to your columns instead (`shownCols`), and
  set the widths again whenever the set changes.
- **Highlighting a table row**: the table leaves a padding between cells,
  so a background per cell shows as separate blocks. Lay the background out
  half a padding wider on each side and the row reads as one.
- **Right-click** needs a widget implementing `fyne.SecondaryTappable`
  (`TappedSecondary`), here the table cell, which then shows a
  `widget.NewPopUpMenu` at the pointer.
- **Menus near the bottom of the window**: show pop-up menus above the
  button (`ShowAtPosition(pos.SubtractXY(0, menu.MinSize().Height))`).
- **Tighter spacing for one part of the UI**: `container.NewThemeOverride`
  with a small theme that changes only padding sizes (`compactTheme`).
- **Name table columns with constants** (`titleCol = iota`, …), never
  bare numbers. When Size was added, a map of "muted" columns written as
  `{3, 4, 6}` quietly went stale and greyed out Quality's slot instead of
  Path's.
- **`widget.List` puts a padding's height between rows**, not a separator's.
  To make a list exactly as tall as its rows, use
  `n·rowHeight + (n−1)·theme.Padding()`.
- **Custom colour names** in the app theme (`colorNameAccent`,
  `colorNameSurface`) keep colours consistent and switchable with the
  light/dark variant.

## 3. Fyne: app identity, preferences, storage

- **The app ID decides where preferences and storage live**
  (`~/Library/Preferences/fyne/<id>/` on macOS). Renaming the app is fine;
  changing the ID loses everyone's settings and saved data, so Mugig keeps
  `org.kiwipanel.musicscan`.
- **Save choices by name, not index.** The sort column was saved as an
  index; adding a column made old settings point at the wrong one. It's now
  saved by name, with old indexes read through the old column order.
- **Version saved files** (`cacheVersion`, `qualityVersion`) and ignore
  ones that don't match, rather than misreading them after a change.
  Adding a field to a gob-encoded struct is safe (it reads as zero); fill
  it from elsewhere if possible instead of discarding the cache.

- **Save state on quit in two places.** Quitting from the menu or with ⌘Q
  runs the app's `Lifecycle().SetOnStopped` hook but doesn't always close
  the window first, so `SetOnClosed` alone can miss it. Save from both,
  once, before the audio player is closed (its position is gone after).
  Preferences set in `OnStopped` are still written to disk.
- **Restore the last session after the library loads**, not at start: the
  saved song has to be found in the library first. Until then, don't save
  over the saved state (here, Up Next).

## 4. Fyne: icons and packaging

- **`fyne package` reshapes the macOS icon itself.** It shrinks the picture
  to 824 of 1024 pixels and masks it to Apple's rounded square. Give it a
  full-bleed square; a picture with its own margin gets shrunk twice and
  looks small in the Dock. (`--use-raw-icon` skips this.)
- **Windows and Linux use the icon as it is**, so they need a rounded icon
  with transparent corners. Mugig has both (`Icon.png`, `IconRounded.png`),
  drawn by `tools/mkicon`, and the Makefile picks one per system.
- **`App.SetIcon` doesn't set the macOS Dock icon** (GLFW ignores window
  icons on macOS). The Dock uses the bundle's icon; when running outside a
  bundle (`go run`), set `NSApp.applicationIconImage` through cgo
  (`dock_darwin.m`).
- **`fyne package --exe` ignores a relative path.** Given `--exe mugig`, it
  quietly built its own binary for this Mac instead. Pass the full path.
- **`fyne package` writes the `.app` into the current directory.** To build
  into `dist/` without overwriting the main `Mugig.app`, run it from there
  with `--src` pointing back at the project.
- **Universal macOS apps**: build arm64 and amd64 with `CGO_ENABLED=1`,
  join them with `lipo -create`, and package that binary. Test the Intel
  half on Apple silicon with `arch -x86_64 …` (Rosetta).
- **Windows and Linux builds need their C compilers** (Fyne and malgo use
  cgo), so they can't be built with plain `GOOS=`. `fyne-cross` builds them
  in Docker; each system's image is 1–2 GB.
- **Binary size**: about 26 MB per architecture. Roughly 18 MB is the Go
  runtime and standard library, 7 MB Fyne, and 7 MB fonts Fyne always
  embeds — including a 4.2 MB colour emoji font, which `-tags no_emoji`
  leaves out. Don't use UPX on macOS (breaks code signing) or Windows
  (antivirus false alarms).

## 5. Fyne: testing

- **`fyne.io/fyne/v2/test`** gives an in-memory app and windows
  (`test.NewTempApp`, `test.NewTempWindow`); `fyne.Do` runs inline there.
  `test.WidgetRenderer(w)` exposes a widget's renderer, so tests can check
  the objects it draws. `test.Tap`, `test.TapSecondary` simulate clicks.
- **Screenshots in tests**: `win.Canvas().Capture()` renders the window to
  an image — useful both for asserting pixels and for looking at a layout
  without running the app.
- **Inject everything that touches the outside world** (`uiDeps`: player,
  cache, media controls, async, the Open panel, revealing files in Finder),
  so tests make no sound, open no windows and run deterministically. Make
  the default fake fail the test if called unexpectedly.
- **Fyne's object cache expires after one minute.** In a long test run (the
  race detector makes it ~90 s) the test window started drawing without
  its cached objects, and a pixel test failed only in the full run. Set
  `FYNE_CACHE=1h` for tests (`make test` does).
- **Theme changes apply in the background** in the test driver; don't
  assume the next capture shows them.
- **Inline `fyne.Do` plus an inline `async` makes background work finish
  before the call that started it returns.** A queue of folder scans then
  ran one scan to the end before the next was queued, unlike in the app.
  Queue all the work first, then start it. To test a moment mid-way, make
  `async` collect the functions and run them one at a time.

## 6. Audio

- **Never read the disk on the audio thread.** The audio callback has to
  deliver sound every few milliseconds (Mugig's device buffer is 3 × 20 ms).
  A USB disk waking up, or a failing disk retrying a bad sector, takes
  200–500 ms. Decoding inside the callback made damaged songs stutter dozens
  of times. A goroutine now decodes up to 3 s ahead into a ring buffer
  (`readahead.go`); the callback only copies. If it ever runs dry, play
  silence rather than ending the song.
- **beep: `Stream` returning `ok == false` ends the track**, whatever the
  reason. Check `Err()` to tell a finished song from a failed one, and say
  so to the user instead of silently moving to the next song.
- **Recover from decoder panics**, both when opening (`decodeAs`) and on the
  audio thread (`safeStream`): a panic there can't be caught anywhere else
  and takes the app down.
- **Keep bit-perfect playback possible**: open the device at the track's
  sample rate (switching the device's own rate on macOS through CoreAudio,
  and restoring it on quit), and pass audio through untouched at full
  volume and 1× speed.
- **Exclusive access on macOS is "hog mode"**
  (`kAudioDevicePropertyHogMode`). Setting it *toggles*, whatever value
  is passed: taken if no one has it, given back if this process has it.
  So read the owner first, and never set it blindly. Take it before
  switching the device's rate, so no other app can change the rate back.
  coreaudiod gives the device back if the process dies, so a crash
  can't leave other apps silent. Mugig's own playback through miniaudio
  carries on while it holds the device (checked on the built-in output).
- **Speed with pitch kept**: WSOLA (`stretch.go`) — overlapping pieces of
  50–100 ms, each joined where its waveform best matches the last, found by
  a coarse then fine correlation search. Cheap (under 1% of a core at
  192 kHz). Bypass it entirely at 1×.
- **Gapless: join the next song inside the audio callback**, not by
  reacting to the end of the last one. The end callback reaches the UI
  thread, which opens the next file and restarts the device: 50–200 ms of
  silence. Instead the UI names the next song 20 s before the end (asked
  again every tick, so changes to Up Next, shuffle or repeat are followed),
  a goroutine opens it, and the streamer (`gapless.go`) moves on to it in
  the same buffer. Only at the same sample rate: another rate needs the
  device reopened. Then the UI's usual "song ended, play the next" call
  finds that song already playing and just claims it, so the play-order
  logic stays in one place.
- **Deliver events from the audio thread in order.** Each `go onUI(f)`
  is its own goroutine, so two of them can arrive in either order: a short
  song's "ended" could beat the "switched to it". Append events to a
  slice under the output lock and have each goroutine drain the slice.
- **MP3s carry silence that must be cut for gapless**: the frame holding
  the Xing/Info tag decodes as 1,152 samples of silence, then come the
  encoder's delay and the decoder's 529 samples. LAME and FFmpeg record the
  delay and the padding at the end in the LAME tag. Checked against the
  WAV the test MP3 was made from: the music starts exactly 1,152 + delay +
  529 samples in. go-mp3 doesn't flush its decoder, so with less than 529
  samples of padding the very end is missing: cap the length at what it
  decodes.
- **go-mp3 puts out up to two frames of noise after a seek**, as the bit
  reservoir and filter bank lack the frames before. Seek 4 frames early
  and decode up to the spot.
- **Get song lengths from the headers, not a decoder.** go-mp3 works out an
  MP3's length by reading every frame, far too slow for a library scan.
  Read it instead from FLAC's STREAMINFO, WAV's data size, the last Ogg
  page's granule position, or the MP3's Xing/VBRI frame count (otherwise
  constant bitrate: audio bytes × 8 ÷ bitrate) (`length.go`). Checked
  against the decoders on 150 real songs: none was off by more than a
  second. The 4,167 songs on two USB disks took 40 s, once.
- **Don't cache "unknown" as an answer** if it might be a passing read
  error. A song cached without a length is measured again on the next scan.
- **malgo's `BackendNull` constant is off by one**; tests use
  `malgo.BackendNull + 1` for miniaudio's silent device.

## 7. mewkiz/flac

- **v1.0.12 allocated 2 GB on a corrupt Vorbis comment**, because
  `NewSeek` parses every metadata block. Fixed by upgrading to v1.0.14;
  fuzzing found it.
- **It logs a note for every frame of a 176.4 kHz or 24 kHz file** through
  the standard logger (its tests don't cover those rates). Filter its
  messages with a log writer (`logfilter.go`) rather than silencing the log,
  which Fyne also uses.
- **`frame.Parse` reads exactly one frame and no further**, so byte offsets
  can be tracked with a counting reader. That makes it possible to skip a
  damaged frame: search for the next sync code (0xFFF8/0xFFF9), parse, and
  accept only a frame whose CRCs pass and whose place in the stream fits
  (`flacplay.go`). Fill the gap with silence to keep timing.
- **`frame.SampleNumber()` is wrong for the last frame of a fixed-block-size
  stream**; use frame number × the stream's block size.
- **Its `Seek` reads every frame from the start when there's no seek table,
  and fails at damage.** Seek by bisection over byte offsets instead,
  finding a good frame at each step.
- **Frames whose header says "sample size: see STREAMINFO"** can't be
  decoded by it; those files go to the macOS decoder (ExtAudioFile).

## 8. Files on real disks

- **macOS litters FAT/exFAT disks** with `._name` AppleDouble files (one
  beside every file) and `__MACOSX` folders from zips. Skip hidden files and
  those folders when scanning; also skip 0-byte files.
- **A failing disk can return different data on every read** of the same
  file. Mugig measured four different SHA-256s for one unmodified file on
  one USB drive. Damage reported by a check may be the drive, not the file.
- **Use the file's own bytes to decide its format** (`sniff`), not its
  extension; misnamed files are common.
- **Watch folders with FSEvents on macOS** (`fsnotify/fsevents`) and
  fsnotify elsewhere; poll to notice a disk being unplugged and plugged back
  in.
- **An unplugged disk looks like an empty folder** to a walk that skips
  errors. Check the folder still exists before treating files the scan
  didn't see as deleted. Otherwise the saved tags of a whole disk are
  forgotten.
- **Tell "disk unplugged" from "folder deleted"** by the nearest folder
  above that still exists: `/Volumes` (or `/media/<user>`, `/mnt`) means the
  disk is gone.
- **Leave out folders whose audio isn't music** once people can add big
  folders: an app bundle copied to a music disk brought 23 VS Code
  interface sounds into the library. Skip bundles (`.app`, `.bundle`, …),
  `node_modules`, `$RECYCLE.BIN`, `~/Library`, but never the folder the
  user chose, and apply the same rule to the folder watcher's events.
- **Listing folders is rarely the slow part of a scan.** Reading tags is.
  A parallel walker (fastwalk) saved milliseconds on real music folders,
  and nothing on a spinning USB disk, where the heads are the limit.
- **Scan with more workers on SSDs than on spinning or USB disks**
  (`diskutil info` tells which on macOS).
- **Fyne's own file dialog doesn't list mounted volumes**; the native macOS
  Open panel does (`pick_darwin.m`).

## 9. Showing a file in the file manager

- macOS: `open -R <path>`.
- Windows: `explorer /select,"<path>"`, passed as the raw command line
  (`SysProcAttr.CmdLine`), because Explorer parses it itself and Go's
  argument quoting breaks it. Explorer exits with status 1 even on success.
- Linux: the freedesktop `org.freedesktop.FileManager1.ShowItems` D-Bus
  call (`dbus-send`, with a `file://` URI), falling back to opening the
  folder with `xdg-open`.
- Check the file still exists first, and explain an unplugged disk.

- **Move to Trash, per system**: macOS `NSFileManager trashItemAtURL:`
  (as Finder; a file on a USB disk goes to that disk's `.Trashes`),
  Windows `SHFileOperationW` with `FOF_ALLOWUNDO` (the Recycle Bin), Linux
  `gio trash` or `trash-put` (the freedesktop.org spec). Kept in its own
  package without Fyne, so `GOOS=windows go vet ./internal/trash` checks
  the Windows code from a Mac, which the app as a whole can't (Fyne needs a
  C compiler for each system).

## 10. cgo and Objective-C

- Put Objective-C in `*_darwin.m` beside a `*_darwin.go` with
  `//go:build darwin`, `#cgo CFLAGS: -x objective-c -fobjc-arc` and the
  frameworks in `LDFLAGS`; give other systems a `*_other.go` stub.
- AppKit calls must run on the main queue: `dispatch_async(dispatch_get_main_queue(), …)`.
- Callbacks into Go (`//export`) arrive on arbitrary threads; hand them to
  the UI with `onUI`.

## 11. Testing and fuzzing

- **Fuzz everything that parses file bytes** (`fuzz_test.go`), with a time
  limit per input. The Go fuzzer reports 0 executions per second while it
  minimises a new input; that's not a hang. To find genuinely slow inputs,
  add a watchdog that saves any input taking over a few seconds.
- **Test DSP against measurable properties**: length ratio, pitch by zero
  crossings, level by RMS, no sample-to-sample jump beyond what a sine can
  do, bit-exact passthrough. Then check real music by comparing average
  spectra.
- **Real-file checks belong in throwaway tests** that read the user's disks
  read-only, are deleted afterwards, and never write there.
- **Bisect order-dependent test failures** by running the failing test with
  halves of the tests before it (a compiled `-race` test binary makes this
  fast).

## 12. Licences

- Don't copy GPL code (e.g. Spek) into the project; learn from it and write
  your own. Credit ideas taken from permissively licensed projects
  (AudioAuditor, Apache 2.0).
- Bundled fonts (Inter) are under the SIL Open Font License; keep its
  licence file beside them.
