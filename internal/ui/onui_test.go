package ui

import "testing"

// UI updates from other goroutines run only on the UI's goroutine: while
// the app shuts down, Fyne would run them on the caller's.
func TestOnUIOnlyOnUIGoroutine(t *testing.T) {
	newTestApp(t)
	ran := 0
	onUI(func() { ran++ }) // tests: no UI goroutine is set, so it runs
	uiGoroutine.Store(goroutineID())
	defer uiGoroutine.Store(0)
	onUI(func() { ran++ }) // on the UI goroutine
	done := make(chan bool)
	go func() {
		onUI(func() { ran++ }) // the test driver runs it here, as Fyne does when shutting down
		close(done)
	}()
	<-done
	if ran != 2 {
		t.Errorf("ran %d times, want 2", ran)
	}
	if goroutineID() == 0 {
		t.Error("no goroutine number")
	}
}
