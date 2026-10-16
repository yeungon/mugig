package radio

// Song titles from Icecast and Shoutcast. Asked for them, a station
// inserts a short note into its sound every so many bytes, as its
// icy-metaint header says: a length byte, times 16, then text such as
// StreamTitle='Artist - Song';. The notes are taken out, so the decoder
// sees only sound.

import (
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type icyReader struct {
	r       io.Reader
	every   int // bytes of sound between notes
	left    int // bytes of sound before the next note
	onTitle func(string)
	title   string // the last title passed on
	note    [255 * 16]byte
}

func (i *icyReader) Read(p []byte) (int, error) {
	if i.left == 0 {
		if err := i.readNote(); err != nil {
			return 0, err
		}
		i.left = i.every
	}
	n, err := i.r.Read(p[:min(len(p), i.left)])
	i.left -= n
	return n, err
}

// readNote reads a note and passes on its title if it has changed. Most
// notes are empty: a zero length byte.
func (i *icyReader) readNote() error {
	var size [1]byte
	if _, err := io.ReadFull(i.r, size[:]); err != nil {
		return err
	}
	note := i.note[:int(size[0])*16]
	if _, err := io.ReadFull(i.r, note); err != nil {
		return err
	}
	if t, ok := streamTitle(note); ok && t != i.title {
		i.title = t
		i.onTitle(t)
	}
	return nil
}

// streamTitle finds StreamTitle='…'; in a note. The title can hold
// apostrophes itself, so it ends at the last "';" before the next field.
func streamTitle(note []byte) (string, bool) {
	s := strings.TrimRight(string(note), "\x00")
	start := strings.Index(s, "StreamTitle='")
	if start < 0 {
		return "", false
	}
	s = s[start+len("StreamTitle='"):]
	end := strings.Index(s, "';Stream") // the next field, such as StreamUrl
	if end < 0 {
		end = strings.LastIndex(s, "';")
	}
	if end < 0 {
		end = strings.LastIndex(s, "'")
	}
	if end >= 0 {
		s = s[:end]
	}
	if !utf8.ValidString(s) { // older stations send Latin-1
		if d, err := charmap.ISO8859_1.NewDecoder().String(s); err == nil {
			s = d
		}
	}
	return strings.TrimSpace(s), true
}
