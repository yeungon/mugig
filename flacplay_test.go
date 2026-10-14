package main

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

const playRate = 44100

// damagedFLAC writes 10 s of stereo noise, then lets damage change the
// file's bytes. It returns the path and the samples written.
func damagedFLAC(t *testing.T, damage func(data []byte) []byte) (string, []int32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.flac")
	s := quantize(noise(10*playRate, playRate, pink), 16)
	writeFLAC(t, path, playRate, 16, s, s)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, damage(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, s
}

// playAll streams path to the end, returning the left channel.
func playAll(t *testing.T, path string) ([]float64, *flacStream) {
	t.Helper()
	s, _, err := decode(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d, ok := s.(*flacStream)
	if !ok {
		t.Fatalf("decoded by %T", s)
	}
	return stream(s), d
}

func stream(s interface {
	Stream([][2]float64) (int, bool)
}) []float64 {
	var out []float64
	buf := make([][2]float64, 1000)
	for {
		n, ok := s.Stream(buf)
		for _, v := range buf[:n] {
			out = append(out, v[0])
		}
		if !ok {
			return out
		}
	}
}

// compare counts samples of got that differ from want, and those that are
// silent where want isn't.
func compare(got []float64, want []int32) (wrong, silent int) {
	for i, v := range got {
		w := float64(want[i]) / 32768
		switch {
		case v == w:
		case v == 0:
			silent++
		default:
			wrong++
		}
	}
	return wrong, silent
}

// One corrupt byte loses only the frame it is in: the song plays to its end,
// with silence for that frame (4096 samples, under a tenth of a second) and
// everything else exactly as written.
func TestPlayPastDamage(t *testing.T) {
	path, want := damagedFLAC(t, func(data []byte) []byte {
		data[len(data)/3] ^= 0x5A
		return data
	})
	got, d := playAll(t, path)
	if len(got) != len(want) {
		t.Fatalf("played %d samples of %d", len(got), len(want))
	}
	wrong, silent := compare(got, want)
	if wrong != 0 || silent < 4000 || silent > 4096 || d.skipped != 1 || d.Err() != nil {
		t.Errorf("%d samples wrong, %d silent, %d skipped, err %v", wrong, silent, d.skipped, d.Err())
	}
}

// A stretch of garbage, as a failing disk returns, loses the frames it
// covers and no more.
func TestPlayPastGarbage(t *testing.T) {
	path, want := damagedFLAC(t, func(data []byte) []byte {
		rand.New(rand.NewSource(1)).Read(data[len(data)/2 : len(data)/2+50_000])
		return data
	})
	got, d := playAll(t, path)
	if len(got) != len(want) {
		t.Fatalf("played %d samples of %d", len(got), len(want))
	}
	wrong, silent := compare(got, want)
	// 50 kB covers 3 frames of 16 kB, and parts of 2 more.
	if wrong != 0 || silent > 5*4096 || d.Err() != nil {
		t.Errorf("%d samples wrong, %d silent, err %v", wrong, silent, d.Err())
	}
}

// A file cut short plays as far as it goes.
func TestPlayTruncated(t *testing.T) {
	path, want := damagedFLAC(t, func(data []byte) []byte { return data[:len(data)*6/10] })
	got, d := playAll(t, path)
	wrong, _ := compare(got, want)
	if len(got) < len(want)/2 || len(got) > len(want)*6/10 || wrong != 0 || d.Err() != nil {
		t.Errorf("played %d of %d samples, %d wrong, err %v", len(got), len(want), wrong, d.Err())
	}
}

// Seeking finds the exact sample, in damaged files too, without reading
// through the damage.
func TestSeekDamaged(t *testing.T) {
	path, want := damagedFLAC(t, func(data []byte) []byte {
		data[len(data)/3] ^= 0x5A
		return data
	})
	s, _, err := decode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	buf := make([][2]float64, 100)
	for _, p := range []int{7 * playRate, 1, 0, 123_457, len(want) - 50, 2 * playRate} {
		if err := s.Seek(p); err != nil {
			t.Fatalf("seek to %d: %v", p, err)
		}
		if s.Position() != p {
			t.Errorf("seek to %d: at %d", p, s.Position())
		}
		n, _ := s.Stream(buf)
		for i := range n {
			if w := float64(want[p+i]) / 32768; buf[i][0] != w {
				t.Fatalf("seek to %d: sample %d is %v, want %v", p, p+i, buf[i][0], w)
			}
		}
	}

	// Into the damaged part: silence until the next good frame, which is
	// where it should be.
	got, d := playAll(t, path)
	lost := -1
	for i, v := range got {
		if v == 0 && want[i] != 0 && lost < 0 {
			lost = i
		}
	}
	d.Seek(lost + 10)
	after := stream(d)
	if len(after) != len(want)-lost-10 {
		t.Errorf("after seeking into the damage, %d samples, want %d", len(after), len(want)-lost-10)
	}
}

// When a song stops because it can't be read, that is said, and the next
// song doesn't start as if it had finished.
func TestStoppedEarlySays(t *testing.T) {
	u, fp, _ := newTestUI(t)
	setLibrary(u, fiveTracks()...)
	u.jump(u.tracks[0])
	fp.fail(t, errors.New("the file couldn't be read: input/output error"))
	if len(fp.played) != 1 || u.playing.Path != "" {
		t.Errorf("played %q, playing %q", fp.played, u.playing.Path)
	}
	if top := u.win.Canvas().Overlays().Top(); top == nil {
		t.Error("no message shown")
	} else if text := allText(top); !strings.Contains(text, "Stopped playing Come Together") {
		t.Errorf("message %q", text)
	}
}

// allText joins the text of every label in o.
func allText(o fyne.CanvasObject) string {
	var out []string
	visit(o, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok {
			out = append(out, l.Text)
		}
	})
	return strings.Join(out, " ")
}
