// Command mkicon draws Mugig's icon: spectrum bars in the spectrogram's
// colours, rising to an "M", on a dark ground. Run it from the project
// folder after changing the design:
//
//	go run ./tools/mkicon
//
// It writes two pictures:
//
//   - Icon.png, the full square. `fyne package` makes the macOS icon from
//     it, adding Apple's margin and rounded corners itself.
//   - IconRounded.png, rounded with transparent corners, for Windows and
//     Linux, whose taskbars show the icon as it is, and the sidebar.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const (
	size    = 1024
	samples = 4 // per pixel, each way, to smooth edges
)

// heights of the bars, as a fraction of the icon: the outer ones tallest,
// dipping to the middle, like an M.
var heights = []float64{0.46, 0.37, 0.28, 0.21, 0.28, 0.37, 0.46}

const (
	baseline = 0.745 // where the bars stand
	spread   = 0.64  // width of all the bars
	gapRatio = 0.34  // gap between bars, as a fraction of a bar's width

	// The rounded square's corner radius, as a fraction of its side: the
	// radius `fyne package` gives macOS icons (185.4 of 824 pixels).
	corner = 0.225
	rim    = 0.007 // width of the light edge that sets the icon off dark backgrounds
)

// heat is the spectrogram's colour scale, from quiet to loud (see
// heatStops in spectrogram.go), without its black.
var heat = []rgb{
	{0x3B, 0x0F, 0x70},
	{0x8C, 0x29, 0x81},
	{0xDE, 0x49, 0x68},
	{0xFE, 0x9F, 0x6D},
	{0xFC, 0xFD, 0xBF},
}

type rgb struct{ r, g, b float64 }

func (c rgb) mix(d rgb, t float64) rgb {
	return rgb{c.r + (d.r-c.r)*t, c.g + (d.g-c.g)*t, c.b + (d.b-c.b)*t}
}

func (c rgb) scale(k float64) rgb { return rgb{c.r * k, c.g * k, c.b * k} }
func (c rgb) add(d rgb) rgb       { return rgb{c.r + d.r, c.g + d.g, c.b + d.b} }

func heatAt(t float64) rgb {
	t = min(max(t, 0), 1) * float64(len(heat)-1)
	i := min(int(t), len(heat)-2)
	return heat[i].mix(heat[i+1], t-float64(i))
}

type bar struct{ x0, x1, top float64 }

func bars() []bar {
	n := float64(len(heights))
	w := spread / (n + (n-1)*gapRatio)
	left := (1 - spread) / 2
	out := make([]bar, len(heights))
	for i, h := range heights {
		x := left + float64(i)*w*(1+gapRatio)
		out[i] = bar{x, x + w, baseline - h}
	}
	return out
}

// capsule returns the distance from (u, v) to bar b, drawn with round ends:
// negative inside.
func (b bar) distance(u, v float64) float64 {
	r := (b.x1 - b.x0) / 2
	cx := b.x0 + r
	cy := min(max(v, b.top+r), baseline-r)
	return math.Hypot(u-cx, v-cy) - r
}

// art returns the colour at (u, v), each 0 to 1 across the square.
func art(u, v float64, bs []bar) rgb {
	// Ground: indigo to near black, lit violet from below the bars.
	c := rgb{0x3A, 0x24, 0x6E}.mix(rgb{0x10, 0x0B, 0x20}, v)
	glow := math.Exp(-(math.Pow((u-0.5)/0.42, 2) + math.Pow((v-0.80)/0.30, 2)))
	c = c.add(rgb{0x7A, 0x24, 0xC8}.scale(0.30 * glow))

	tall := heights[0]
	for _, b := range bs {
		d := b.distance(u, v)
		// The bars' own light on the ground around them.
		bloom := math.Exp(-math.Pow(max(d, 0)/0.035, 2))
		lit := heatAt(0.2 + 0.8*(baseline-v)/tall) // from magenta, not the ground's violet
		c = c.add(lit.scale(0.16 * bloom))
		if d < 0 {
			c = lit
		}
	}
	// A faint light edge, strongest at the top, as if lit from above.
	if e := -roundedSquare(u, v); e < rim {
		k := 1 - e/rim
		c = c.mix(rgb{255, 255, 255}, 0.22*k*(1-0.75*v))
	}
	return c
}

// roundedSquare returns the distance from (u, v) to the edge of the
// rounded square filling the unit square: negative inside.
func roundedSquare(u, v float64) float64 {
	x := math.Abs(u-0.5) - (0.5 - corner)
	y := math.Abs(v-0.5) - (0.5 - corner)
	outside := math.Hypot(max(x, 0), max(y, 0))
	return outside + min(max(x, y), 0) - corner
}

// draw renders the icon. With rounded set, the art is fitted into a rounded
// square with transparent corners.
func draw(rounded bool) *image.NRGBA {
	const margin = 0.03 // around the rounded square; dock_darwin.m allows for it
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	bs := bars()
	for py := range size {
		for px := range size {
			var sum rgb
			cover := 0.0
			for sy := range samples {
				for sx := range samples {
					u := (float64(px) + (float64(sx)+0.5)/samples) / size
					v := (float64(py) + (float64(sy)+0.5)/samples) / size
					au, av := u, v
					if rounded {
						// The art fills a rounded square inset by margin.
						au = (u - margin) / (1 - 2*margin)
						av = (v - margin) / (1 - 2*margin)
						if roundedSquare(au, av) > 0 {
							continue
						}
					}
					sum = sum.add(art(au, av, bs))
					cover++
				}
			}
			if cover == 0 {
				continue
			}
			c := sum.scale(1 / cover)
			img.SetNRGBA(px, py, color.NRGBA{
				byte(min(c.r, 255)), byte(min(c.g, 255)), byte(min(c.b, 255)),
				byte(math.Round(255 * cover / (samples * samples))),
			})
		}
	}
	return img
}

func save(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	save("Icon.png", draw(false))
	save("IconRounded.png", draw(true))
}
