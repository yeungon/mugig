package trash

import (
	"os"
	"path/filepath"
	"testing"
)

// A file moved to the Trash is gone from where it was. It puts a small
// file in the real Trash, so it only runs when asked for:
// MUGIG_TRASH_TEST=1 go test ./internal/trash
func TestMove(t *testing.T) {
	if os.Getenv("MUGIG_TRASH_TEST") == "" {
		t.Skip("set MUGIG_TRASH_TEST=1 to put a test file in the Trash")
	}
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, "mugig-trash-test-") // the temp dir may have no Trash
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "mugig-trash-test.txt")
	if err := os.WriteFile(path, []byte("Mugig's trash test; safe to delete"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Move(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("still there: %v", err)
	}
	if err := Move(filepath.Join(dir, "missing")); err == nil {
		t.Error("moving a missing file should fail")
	}
}
