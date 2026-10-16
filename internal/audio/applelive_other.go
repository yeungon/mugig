//go:build !darwin

package audio

import (
	"io"

	"mugig/internal/radio"

	"github.com/gopxl/beep/v2"
)

func appleLiveDecode(io.Reader, radio.Codec) (beep.StreamSeekCloser, beep.Format, error) {
	return nil, beep.Format{}, errNoApple
}
