package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSwap(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "Mugig.exe"), "old")
	next := writeFile(t, filepath.Join(dir, "work", "Mugig.exe"), "new")
	if err := swap(target, next, filepath.Join(dir, "work", "old.exe")); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new" || readFile(t, filepath.Join(dir, "work", "old.exe")) != "old" {
		t.Error("not swapped")
	}

	// When the new one can't be put in place, the old one is put back.
	if err := swap(target, filepath.Join(dir, "missing"), filepath.Join(dir, "work", "old2.exe")); err == nil {
		t.Error("swapped in a missing file")
	}
	if readFile(t, target) != "new" {
		t.Error("not put back")
	}
}

// Started by Restart, a copy waits for the one it replaces to quit.
func TestAfterRestartWaits(t *testing.T) {
	if os.Getenv("MUGIG_TEST_SLEEP") != "" {
		time.Sleep(500 * time.Millisecond)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAfterRestartWaits$")
	cmd.Env = append(os.Environ(), "MUGIG_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Setenv(waitEnv, strconv.Itoa(cmd.Process.Pid))
	AfterRestart()
	select {
	case <-done:
	default:
		t.Error("didn't wait for the old copy")
	}
	if os.Getenv(waitEnv) != "" {
		t.Error("left the variable set, for copies it starts")
	}
}

// Without a copy to wait for, it doesn't.
func TestAfterRestartNoWait(t *testing.T) {
	t.Setenv(waitEnv, "")
	start := time.Now()
	AfterRestart()
	if time.Since(start) > time.Second {
		t.Error("waited")
	}
}

// Run from go test, Mugig isn't installed from a release.
func TestTargetFromSource(t *testing.T) {
	if _, err := Target(); err == nil {
		t.Error("the test program could be replaced")
	}
}
