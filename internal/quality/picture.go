package quality

import (
	"bytes"
	"image"
	"image/png"
	"math"
)

const (
	SpecRows     = 256 // frequency rows, from 0 Hz to half the sample rate
	PictureWidth = 720 // time columns

	// A picture keeps each point's level, from 0 dBFS down to KeptFloorDB,
	// so it can be drawn for any range of levels without analysing again.
	KeptFloorDB = -140
)

// picture returns the spectrogram's levels, or nil if no audio was
// analysed. Each pixel is a level: 0 for KeptFloorDB or quieter, up to 255
// for full scale. paint turns it into colours.
func (s *spectrum) picture() *image.Gray {
	n := len(s.columns)
	if n == 0 {
		return nil
	}
	img := image.NewGray(image.Rect(0, 0, PictureWidth, SpecRows))
	fullScale := math.Pow(specSize/4, 2) // a full-scale tone's bin, after the Hann window
	binsPerRow := float64(specSize / 2 / SpecRows)
	for x := range PictureWidth {
		a := x * n / PictureWidth
		b := max((x+1)*n/PictureWidth, a+1)
		for r := range SpecRows {
			p := 0.0
			for _, col := range s.columns[a:b] {
				p += float64(col[r])
			}
			p /= float64(b-a) * binsPerRow
			db := 10 * math.Log10(p/fullScale+1e-30)
			v := math.Round((db - KeptFloorDB) / -KeptFloorDB * 255)
			img.Pix[(SpecRows-1-r)*img.Stride+x] = uint8(min(max(v, 0), 255))
		}
	}
	return img
}

// LevelDB is the level a picture's pixel value stands for.
func LevelDB(v uint8) float64 { return KeptFloorDB * (1 - float64(v)/255) }

// Levels are saved losslessly, as a greyscale PNG.
func encodePicture(levels *image.Gray) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, levels)
	return buf.Bytes(), err
}

func decodePicture(data []byte) (*image.Gray, bool) {
	img, err := png.Decode(bytes.NewReader(data))
	g, ok := img.(*image.Gray)
	return g, err == nil && ok && g.Bounds().Dx() > 0
}
