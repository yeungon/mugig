package main

// Check All Songs: every lossless song in the library without a saved
// result is checked in the background, a few at a time, filling in the
// Quality column as it goes. MP3 and Ogg files are lossy by format, so
// there is nothing to verify in them.

import (
	"context"
	"fmt"
	"mugig/internal/humanize"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	batchSaveEvery = 50                     // results between saves
	batchShowEvery = 500 * time.Millisecond // how often progress is shown
)

// batchCheck is a Check All Songs run. Its fields are used on the UI thread.
type batchCheck struct {
	cancel   context.CancelFunc
	total    int // songs to go through
	done     int // songs gone through, saved results included
	analysed int // songs actually decoded, for the time estimate
	counts   map[qualityLevel]int
	started  time.Time
	finished bool
}

// batchResult is one song's outcome, from a worker.
type batchResult struct {
	path   string
	level  qualityLevel
	reused bool // a saved result, so it took no time
}

// buildBatchBar creates the Songs page's Check All Songs button and the
// progress line shown while it runs.
func (u *ui) buildBatchBar() (button, bar fyne.CanvasObject) {
	u.checkAllBtn = widget.NewButtonWithIcon("Check All Songs", theme.SearchIcon(), u.checkAllSongs)
	u.checkAllBtn.Importance = widget.LowImportance
	u.batchProgress = widget.NewProgressBar()
	u.batchLabel = secondaryLabel("")
	u.batchStop = widget.NewButtonWithIcon("", theme.CancelIcon(), u.stopBatch)
	u.batchStop.Importance = widget.LowImportance
	u.batchBar = container.NewBorder(nil, nil, nil, u.batchStop,
		container.NewVBox(u.batchLabel, u.batchProgress))
	u.batchBar.Hide()
	return container.NewVBox(layout.NewSpacer(), u.checkAllBtn, layout.NewSpacer()), u.batchBar
}

// batchWorkers is how many songs to check at once: as many as the
// slowest disk can feed, keeping a processor free for playback and the
// window.
func (u *ui) batchWorkers() int {
	n, known := max(1, runtime.NumCPU()-1), false
	for _, f := range u.folders {
		if f.connected {
			n, known = min(n, scanWorkers(f.path)), true
		}
	}
	if !known {
		n = min(n, gentleWorkers) // songs opened on their own: the disk is unknown
	}
	return max(1, n)
}

// checkAllSongs starts checking every lossless song.
func (u *ui) checkAllSongs() {
	u.checkSongs(u.all)
}

// checkSongs starts checking the lossless songs among songs.
func (u *ui) checkSongs(songs []Track) {
	if u.batch != nil && !u.batch.finished {
		return // already running
	}
	// The songs of a disc image share its file, which is checked once.
	var todo []Track
	files := map[string]bool{}
	for _, t := range songs {
		if f := fileOf(t.Path); !lossyFormats[t.Format] && !files[f] {
			files[f] = true
			todo = append(todo, t)
		}
	}
	if len(todo) == 0 {
		u.batchLabel.SetText("No lossless songs to check")
		u.batchProgress.Hide()
		u.batchBar.Show()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &batchCheck{cancel: cancel, total: len(todo), counts: map[qualityLevel]int{}, started: time.Now()}
	u.batch = b
	u.checkAllBtn.Disable()
	u.checkAllItem.Disabled = true
	u.mainMenu.Refresh()
	u.batchProgress.Show()
	u.batchStop.Show()
	u.batchBar.Show()
	u.showBatch()
	workers := u.batchWorkers()
	u.async(func() { u.runBatch(ctx, b, todo, workers) })
}

func (u *ui) stopBatch() {
	if b := u.batch; b != nil && !b.finished {
		b.cancel()
		return
	}
	u.batchBar.Hide() // dismiss the summary
}

// runBatch checks the songs with a pool of workers, handing results to the
// UI in batches. It runs off the UI thread and returns when done.
func (u *ui) runBatch(ctx context.Context, b *batchCheck, todo []Track, workers int) {
	jobs := make(chan Track)
	results := make(chan batchResult, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				if r, ok := u.checkOne(ctx, fileOf(t.Path)); ok {
					results <- r
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, t := range todo {
			select {
			case jobs <- t:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var pending []batchResult
	lastShown, sinceSave := time.Now(), 0
	flush := func() {
		got := pending
		pending = nil
		onUI(func() { u.batchProgressed(b, got) })
	}
	for r := range results {
		pending = append(pending, r)
		if !r.reused {
			if sinceSave++; sinceSave == batchSaveEvery {
				u.qualityStore.save()
				sinceSave = 0
			}
		}
		if time.Since(lastShown) >= batchShowEvery {
			flush()
			lastShown = time.Now()
		}
	}
	u.qualityStore.save()
	flush()
	onUI(func() { u.batchFinished(b, ctx.Err() != nil) })
}

// checkOne checks one song, or reuses its saved result. ok is false if the
// run was cancelled before it finished.
func (u *ui) checkOne(ctx context.Context, path string) (r batchResult, ok bool) {
	r.path = path
	stamp := fileStamp(path)
	if e, found := u.qualityStore.lookup(path, stamp); found {
		r.level, r.reused = e.Level, true
		return r, true
	}
	info, err := probe(path)
	if err != nil || !canVerify(info) {
		return r, true // unreadable or not checkable: stays unknown
	}
	rep, _, err := analyse(ctx, path, info, false, nil)
	if err != nil {
		return r, false // cancelled
	}
	r.level = assess(info, &rep).level
	u.qualityStore.putQuietly(path, stamp, rep, r.level)
	return r, true
}

// batchProgressed shows results as they come in.
func (u *ui) batchProgressed(b *batchCheck, got []batchResult) {
	for _, r := range got {
		b.done++
		if !r.reused {
			b.analysed++
		}
		b.counts[r.level]++
		if r.level != qualityUnknown {
			u.levels[r.path] = r.level
		}
	}
	if len(got) > 0 {
		u.table.Refresh()
	}
	if u.batch == b {
		u.showBatch()
	}
}

func (u *ui) batchFinished(b *batchCheck, cancelled bool) {
	b.finished = true
	if u.batch != b {
		return
	}
	u.checkAllBtn.Enable()
	u.checkAllItem.Disabled = false
	u.mainMenu.Refresh()
	u.batchProgress.Hide()
	u.showBatch()
	if !cancelled {
		u.batchLabel.SetText(u.batchLabel.Text + ". Sort by Quality to see problems first")
	}
}

// showBatch updates the progress line: how far, what was found, and how
// long is left.
func (u *ui) showBatch() {
	b := u.batch
	u.batchProgress.SetValue(float64(b.done) / float64(b.total))
	var found []string
	for _, l := range []qualityLevel{qualityDamaged, qualitySuspect, qualityLossless, qualityHiRes} {
		if n := b.counts[l]; n > 0 {
			found = append(found, fmt.Sprintf("%s %s", humanize.Thousands(n), strings.ToLower(levelNames[l])))
		}
	}
	verb := "Checking"
	if b.finished {
		verb = "Checked"
		if b.done < b.total {
			verb = "Stopped after"
		}
	}
	text := fmt.Sprintf("%s %s of %s songs", verb, humanize.Thousands(b.done), humanize.Thousands(b.total))
	if len(found) > 0 {
		text += ": " + strings.Join(found, " · ")
	}
	if left := b.timeLeft(); left > 0 && !b.finished {
		text += " · " + roughly(left) + " left"
	}
	u.batchLabel.SetText(text)
}

// timeLeft estimates the rest from the pace of the songs decoded so far,
// once there are enough to judge by. Songs with saved results take no time
// and are left out.
func (b *batchCheck) timeLeft() time.Duration {
	if b.analysed < 5 {
		return 0
	}
	per := time.Since(b.started) / time.Duration(b.analysed)
	return per * time.Duration(b.total-b.done)
}

// roughly formats a duration for an estimate, e.g. "about 25 min".
func roughly(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("about %d min", int(d.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("about %.1f hours", d.Hours())
}
