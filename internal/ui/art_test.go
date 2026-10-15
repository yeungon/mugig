package ui

import (
	"bytes"
	"image"
	"image/png"
	"mugig/internal/audiotest"
	"mugig/internal/library"
	"path/filepath"
	"testing"
)

func TestPlayingShowsArt(t *testing.T) {
	u, _, _ := newTestUI(t)
	dir := t.TempDir()
	withArt := library.Track{Path: filepath.Join(dir, "a", "song.mp3"), Title: "With art"}
	without := library.Track{Path: filepath.Join(dir, "b", "song.mp3"), Title: "Without"}
	audiotest.WriteFile(t, withArt.Path, nil)
	audiotest.WriteFile(t, filepath.Join(dir, "a", "cover.png"), tinyPNG(t))
	audiotest.WriteFile(t, without.Path, nil)
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
