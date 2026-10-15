package audio

import (
	"math"
	"math/cmplx"
)

// FFT replaces x with its discrete Fourier transform. len(x) must be a power
// of two.
func FFT(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ { // reorder by bit-reversed index
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*w
				x[start+k], x[start+k+size/2] = a+b, a-b
				w *= step
			}
		}
	}
}
