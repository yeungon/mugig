//go:build darwin

package main

import "testing"

func TestPlistSolidState(t *testing.T) {
	for plist, want := range map[string]bool{
		"<key>SolidState</key>\n\t<true/>":  true,
		"<key>SolidState</key>\n\t<false/>": false,
		"<key>Removable</key><true/>":       false, // a USB disk that doesn't say
		"":                                  false,
	} {
		if got := plistSolidState(plist); got != want {
			t.Errorf("%q: got %v", plist, got)
		}
	}
}

func TestScanWorkers(t *testing.T) {
	if n := scanWorkers("/no/such/folder"); n != gentleWorkers {
		t.Errorf("unknown drive: %d workers, want %d", n, gentleWorkers)
	}
	// Whatever this machine's disk is, the answer is one of the two.
	if n := scanWorkers(t.TempDir()); n != gentleWorkers && n != parallelWorkers() {
		t.Errorf("%d workers", n)
	}
}

// A folder gets the same answer as its volume's root: diskutil itself only
// accepts volumes, so the folder's mount point must be looked up.
func TestFolderJudgedByItsVolume(t *testing.T) {
	if folder, volume := solidState(t.TempDir()), solidState("/"); folder != volume {
		t.Errorf("temp folder solid-state %v, but its disk %v", folder, volume)
	}
}
