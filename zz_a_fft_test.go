package main

import (
	"math"
	"math/cmplx"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/gopxl/beep/v2"
)

func TestFFTMatchesDirectDFT(t *testing.T) {
	const n = 16
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(rand.Float64()-0.5, rand.Float64()-0.5)
	}
	want := make([]complex128, n)
	for k := range want {
		for j, v := range x {
			want[k] += v * cmplx.Exp(complex(0, -2*math.Pi*float64(j*k)/n))
		}
	}
	FFT(x)
	for k := range x {
		if cmplx.Abs(x[k]-want[k]) > 1e-9 {
			t.Fatalf("bin %d = %v, want %v", k, x[k], want[k])
		}
	}
}

func TestTapKeepsLatestSamples(t *testing.T) {
	n := 0
	counter := beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		for i := range s {
			s[i] = [2]float64{float64(n), float64(n)}
			n++
		}
		return len(s), true
	})
	tp := newTap(counter, 8)
	buf := make([][2]float64, 5)
	tp.Stream(buf)
	tp.Stream(buf) // 10 samples through a ring of 8: wraps
	if buf[4][0] != 9 {
		t.Fatal("tap should pass audio through unchanged")
	}
	got := make([]float64, 4)
	tp.latest(got)
	if !slices.Equal(got, []float64{6, 7, 8, 9}) {
		t.Errorf("latest = %v, want [6 7 8 9]", got)
	}
}
