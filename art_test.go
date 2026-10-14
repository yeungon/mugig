package main

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"
)

func TestCoverArtEmbedded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	writeFile(t, path, id3v23(map[string]string{"TIT2": "Song", "APIC": "embedded-image"}))
	writeFile(t, filepath.Join(filepath.Dir(path), "cover.jpg"), []byte("folder-image"))

	if got := coverArt(path); string(got) != "embedded-image" {
		t.Errorf("coverArt = %q, want the embedded picture over the folder one", got)
	}
}

func TestCoverArtFromFolder(t *testing.T) {
	for _, name := range []string{"cover.jpg", "Folder.PNG", "front.jpeg", "AlbumArt.jpg"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			song := filepath.Join(dir, "song.flac")
			writeFile(t, song, []byte("untagged"))
			writeFile(t, filepath.Join(dir, "scan of booklet.jpg"), []byte("not a cover"))
			writeFile(t, filepath.Join(dir, name), []byte("cover-image"))
			if got := coverArt(song); string(got) != "cover-image" {
				t.Errorf("coverArt = %q", got)
			}
		})
	}
}

func TestCoverArtNone(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "song.mp3")
	writeFile(t, song, id3v23(map[string]string{"TIT2": "No art"}))
	writeFile(t, filepath.Join(dir, "cover.txt"), []byte("not an image"))
	if got := coverArt(song); got != nil {
		t.Errorf("coverArt = %q, want nil", got)
	}
	if got := coverArt(filepath.Join(dir, "gone", "missing.mp3")); got != nil {
		t.Errorf("missing file: coverArt = %q, want nil", got)
	}
}

func TestPlayingShowsArt(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := t.TempDir()
	withArt := Track{Path: filepath.Join(dir, "a", "song.mp3"), Title: "With art"}
	without := Track{Path: filepath.Join(dir, "b", "song.mp3"), Title: "Without"}
	writeFile(t, withArt.Path, nil)
	writeFile(t, filepath.Join(dir, "a", "cover.png"), tinyPNG(t))
	writeFile(t, without.Path, nil)
	setLibrary(u, withArt, without)

	u.jump(withArt)
	if !bytes.Equal(u.artData, tinyPNG(t)) || !bytes.Equal(lastMedia(u).Art, u.artData) {
		t.Errorf("art = %q, published %q", u.artData, lastMedia(u).Art)
	}
	u.next()
	if u.artData != nil || lastMedia(u).Art != nil {
		t.Error("a track without art should clear the previous cover")
	}
	u.stop()
	if u.artData != nil {
		t.Error("stop should clear the art")
	}
}

// tinyPNG is a real 2×2 image, so showing it logs no decoding errors.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
