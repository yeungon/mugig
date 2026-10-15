package gobfile

import (
	"os"
	"path/filepath"
	"testing"
)

// What Save writes, Load reads back, and no temporary file is left over.
func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "data.gob")
	in := map[string]int{"a": 1, "b": 2}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	var out map[string]int
	if err := Load(path, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out["a"] != 1 || out["b"] != 2 {
		t.Errorf("loaded %v", out)
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 1 {
		t.Errorf("%d files left beside it", len(files))
	}
	if err := Load(filepath.Join(t.TempDir(), "missing.gob"), &out); err == nil {
		t.Error("loading a missing file should fail")
	}
}
