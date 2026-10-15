package library

import (
	"mugig/internal/audiotest"
	"path/filepath"
	"testing"
)

func TestCoverArtEmbedded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	audiotest.WriteFile(t, path, audiotest.ID3v23(map[string]string{"TIT2": "Song", "APIC": "embedded-image"}))
	audiotest.WriteFile(t, filepath.Join(filepath.Dir(path), "cover.jpg"), []byte("folder-image"))

	if got := CoverArt(path); string(got) != "embedded-image" {
		t.Errorf("coverArt = %q, want the embedded picture over the folder one", got)
	}
}

func TestCoverArtFromFolder(t *testing.T) {
	for _, name := range []string{"cover.jpg", "Folder.PNG", "front.jpeg", "AlbumArt.jpg"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			song := filepath.Join(dir, "song.flac")
			audiotest.WriteFile(t, song, []byte("untagged"))
			audiotest.WriteFile(t, filepath.Join(dir, "scan of booklet.jpg"), []byte("not a cover"))
			audiotest.WriteFile(t, filepath.Join(dir, name), []byte("cover-image"))
			if got := CoverArt(song); string(got) != "cover-image" {
				t.Errorf("coverArt = %q", got)
			}
		})
	}
}

func TestCoverArtNone(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "song.mp3")
	audiotest.WriteFile(t, song, audiotest.ID3v23(map[string]string{"TIT2": "No art"}))
	audiotest.WriteFile(t, filepath.Join(dir, "cover.txt"), []byte("not an image"))
	if got := CoverArt(song); got != nil {
		t.Errorf("coverArt = %q, want nil", got)
	}
	if got := CoverArt(filepath.Join(dir, "gone", "missing.mp3")); got != nil {
		t.Errorf("missing file: coverArt = %q, want nil", got)
	}
}
