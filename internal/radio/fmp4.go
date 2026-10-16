package radio

// Fragmented MP4, as newer HLS pieces are. An initialization section says
// what each track holds; each piece is then a moof box, saying where each
// frame of sound is and how long, and an mdat box holding them. AAC frames
// get an ADTS header each, made from the track's AudioSpecificConfig, so
// they read as an ADTS stream; MP3 frames carry their own headers.

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// fmp4 takes the sound out of fragmented MP4 pieces.
type fmp4 struct {
	track       uint32 // the sound's track ID
	codec       Codec
	adts        [7]byte // the ADTS header for AAC frames, less their length
	defaultSize uint32  // a frame's size when a piece doesn't say
}

// boxes calls fn with each box in b, stopping if fn returns false. start is
// where the box begins within b.
func boxes(b []byte, fn func(kind string, body []byte, start int) bool) {
	for at := 0; at+8 <= len(b); {
		size, header := uint64(binary.BigEndian.Uint32(b[at:])), 8
		switch size {
		case 1:
			if at+16 > len(b) {
				return
			}
			size, header = binary.BigEndian.Uint64(b[at+8:]), 16
		case 0:
			size = uint64(len(b) - at)
		}
		if size < uint64(header) || size > uint64(len(b)-at) {
			return
		}
		if !fn(string(b[at+4:at+8]), b[at+header:at+int(size)], at) {
			return
		}
		at += int(size)
	}
}

// child is the box at path within b, taking the first of each kind, or
// nil.
func child(b []byte, path ...string) []byte {
	for _, kind := range path {
		var found []byte
		boxes(b, func(k string, body []byte, _ int) bool {
			if k == kind {
				found = body
				return false
			}
			return true
		})
		if found == nil {
			return nil
		}
		b = found
	}
	return b
}

// isMP4 reports whether b begins with an MP4 box a piece could start with.
func isMP4(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	switch string(b[4:8]) {
	case "moof", "styp", "sidx", "emsg", "prft", "ftyp", "free":
		return true
	}
	return false
}

// parseInit reads an initialization section, finding its first track of
// sound.
func parseInit(b []byte) (*fmp4, error) {
	moov := child(b, "moov")
	if moov == nil {
		return nil, errors.New("the station's MP4 initialization section has no tracks")
	}
	f := &fmp4{}
	var unsupported string
	boxes(moov, func(kind string, trak []byte, _ int) bool {
		if kind != "trak" {
			return true
		}
		if hdlr := child(trak, "mdia", "hdlr"); len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
			return true
		}
		tkhd := child(trak, "tkhd")
		stsd := child(trak, "mdia", "minf", "stbl", "stsd")
		if len(tkhd) < 24 || len(stsd) < 8 {
			return true
		}
		if tkhd[0] == 1 {
			f.track = binary.BigEndian.Uint32(tkhd[20:])
		} else {
			f.track = binary.BigEndian.Uint32(tkhd[12:])
		}
		var err error
		boxes(stsd[8:], func(entry string, body []byte, _ int) bool {
			switch entry {
			case "mp4a":
				err = f.readMP4A(body)
			case ".mp3":
				f.codec = MP3
			default:
				unsupported = entry
			}
			return false // the first entry
		})
		if err != nil {
			unsupported = err.Error()
		}
		return f.codec == ""
	})
	if f.codec == "" {
		if unsupported != "" {
			return nil, fmt.Errorf("the station's sound is %q, which Mugig can't play", unsupported)
		}
		return nil, errors.New("the station's MP4 stream has no sound")
	}
	if trex := findTrex(moov, f.track); len(trex) >= 20 {
		f.defaultSize = binary.BigEndian.Uint32(trex[16:])
	}
	return f, nil
}

func findTrex(moov []byte, track uint32) []byte {
	var found []byte
	boxes(child(moov, "mvex"), func(kind string, body []byte, _ int) bool {
		if kind == "trex" && len(body) >= 20 && binary.BigEndian.Uint32(body[4:]) == track {
			found = body
			return false
		}
		return true
	})
	return found
}

// readMP4A reads an mp4a sample entry: AAC, or MP3 described the MPEG-4
// way.
func (f *fmp4) readMP4A(body []byte) error {
	// The fixed fields come first: 28 bytes, more in QuickTime's versions 1 and 2.
	if len(body) < 28 {
		return errors.New("a damaged mp4a entry")
	}
	skip := 28
	switch binary.BigEndian.Uint16(body[8:]) {
	case 1:
		skip += 16
	case 2:
		skip += 36
	}
	if skip > len(body) {
		return errors.New("a damaged mp4a entry")
	}
	esds := child(body[skip:], "esds")
	if len(esds) < 4 {
		return errors.New("an mp4a entry without its esds")
	}
	oti, config := readESDS(esds[4:])
	switch oti {
	case 0x40, 0x66, 0x67, 0x68: // MPEG-4 and MPEG-2 AAC
		header, err := adtsHeader(config)
		if err != nil {
			return err
		}
		f.adts, f.codec = header, AAC
	case 0x69, 0x6B: // MPEG-2 and MPEG-1 audio
		f.codec = MP3
	default:
		return fmt.Errorf("MPEG-4 sound of type %#x", oti)
	}
	return nil
}

// readESDS reads an ES descriptor: the kind of sound (its object type
// indication), and the decoder's configuration.
func readESDS(b []byte) (oti byte, config []byte) {
	tag, body := descriptor(b)
	if tag != 0x03 || len(body) < 3 {
		return 0, nil
	}
	flags, at := body[2], 3
	if flags&0x80 != 0 {
		at += 2
	}
	if flags&0x40 != 0 && at < len(body) {
		at += 1 + int(body[at])
	}
	if flags&0x20 != 0 {
		at += 2
	}
	if at > len(body) {
		return 0, nil
	}
	tag, dcd := descriptor(body[at:])
	if tag != 0x04 || len(dcd) < 13 {
		return 0, nil
	}
	if tag, dsi := descriptor(dcd[13:]); tag == 0x05 {
		config = dsi
	}
	return dcd[0], config
}

// descriptor reads an MPEG-4 descriptor: a tag, and a length of one to
// four bytes, seven bits each.
func descriptor(b []byte) (tag byte, body []byte) {
	if len(b) < 2 {
		return 0, nil
	}
	size, at := 0, 1
	for at < len(b) && at <= 4 {
		size = size<<7 | int(b[at]&0x7F)
		at++
		if b[at-1]&0x80 == 0 {
			break
		}
	}
	if at+size > len(b) {
		return 0, nil
	}
	return b[0], b[at : at+size]
}

// adtsHeader makes the ADTS header for frames of AAC as config, an
// AudioSpecificConfig, describes them. For HE-AAC, which adds SBR or
// parametric stereo, the header describes the plain AAC underneath; the
// decoder finds the rest in the frames, as with any HE-AAC stream.
func adtsHeader(config []byte) ([7]byte, error) {
	var h [7]byte
	r := bitReader{b: config}
	aot := r.objectType()
	freq := r.bits(4)
	if freq == 15 {
		return h, errors.New("AAC at an unusual sample rate")
	}
	channels := r.bits(4)
	if aot == 5 || aot == 29 { // SBR or parametric stereo: the base comes after
		if r.bits(4) == 15 {
			r.bits(24)
		}
		aot = r.objectType()
	}
	if r.failed || aot < 1 || aot > 4 || channels > 7 {
		return h, errors.New("a kind of AAC that ADTS can't carry")
	}
	h[0], h[1] = 0xFF, 0xF1 // MPEG-4, no checksum
	h[2] = byte(aot-1)<<6 | byte(freq)<<2 | byte(channels>>2)
	h[3] = byte(channels&3) << 6
	h[5], h[6] = 0x1F, 0xFC // buffer fullness: variable
	return h, nil
}

type bitReader struct {
	b      []byte
	at     int // in bits
	failed bool
}

func (r *bitReader) bits(n int) int {
	v := 0
	for range n {
		if r.at/8 >= len(r.b) {
			r.failed = true
			return 0
		}
		v = v<<1 | int(r.b[r.at/8]>>(7-r.at%8)&1)
		r.at++
	}
	return v
}

func (r *bitReader) objectType() int {
	if t := r.bits(5); t != 31 {
		return t
	}
	return 32 + r.bits(6)
}

// extract takes the sound's frames out of a piece.
func (f *fmp4) extract(b []byte) ([]byte, error) {
	var out []byte
	var err error
	boxes(b, func(kind string, moof []byte, start int) bool {
		if kind == "moof" {
			out, err = f.fragment(b, moof, start, out)
		}
		return err == nil
	})
	return out, err
}

// fragment adds the frames that moof, at start in piece, places.
func (f *fmp4) fragment(piece, moof []byte, start int, out []byte) ([]byte, error) {
	var err error
	boxes(moof, func(kind string, traf []byte, _ int) bool {
		if kind != "traf" {
			return true
		}
		tfhd := child(traf, "tfhd")
		if len(tfhd) < 8 || binary.BigEndian.Uint32(tfhd[4:]) != f.track {
			return true
		}
		flags, at := binary.BigEndian.Uint32(tfhd)&0xFFFFFF, 8
		base, size := int64(start), f.defaultSize // where offsets count from: the moof, usually
		field := func() uint32 {
			if at+4 > len(tfhd) {
				return 0
			}
			v := binary.BigEndian.Uint32(tfhd[at:])
			at += 4
			return v
		}
		if flags&0x01 != 0 {
			hi := field()
			base = int64(hi)<<32 | int64(field())
		}
		if flags&0x02 != 0 {
			field()
		}
		if flags&0x08 != 0 {
			field()
		}
		if flags&0x10 != 0 {
			size = field()
		}
		next := base // a run without an offset follows the one before
		boxes(traf, func(kind string, trun []byte, _ int) bool {
			if kind == "trun" {
				out, next, err = f.run(piece, trun, base, next, size, out)
			}
			return err == nil
		})
		return err == nil
	})
	return out, err
}

// run adds the frames of a trun box.
func (f *fmp4) run(piece, trun []byte, base, next int64, size uint32, out []byte) ([]byte, int64, error) {
	if len(trun) < 8 {
		return out, next, nil
	}
	flags, count, at := binary.BigEndian.Uint32(trun)&0xFFFFFF, binary.BigEndian.Uint32(trun[4:]), 8
	field := func() uint32 {
		if at+4 > len(trun) {
			return 0
		}
		v := binary.BigEndian.Uint32(trun[at:])
		at += 4
		return v
	}
	pos := next
	if flags&0x01 != 0 {
		pos = base + int64(int32(field()))
	}
	if flags&0x04 != 0 {
		field()
	}
	for range count {
		if flags&0x100 != 0 {
			field()
		}
		n := size
		if flags&0x200 != 0 {
			n = field()
		}
		if flags&0x400 != 0 {
			field()
		}
		if flags&0x800 != 0 {
			field()
		}
		if pos < 0 || pos+int64(n) > int64(len(piece)) {
			return out, pos, errors.New("the station sent a damaged MP4 piece")
		}
		frame := piece[pos : pos+int64(n)]
		if f.codec == AAC {
			h := f.adts
			length := len(frame) + len(h)
			h[3] |= byte(length >> 11 & 3)
			h[4] = byte(length >> 3)
			h[5] |= byte(length&7) << 5
			out = append(out, h[:]...)
		}
		out = append(out, frame...)
		pos += int64(n)
	}
	return out, pos, nil
}
