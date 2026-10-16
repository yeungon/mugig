package radio

// MPEG transport streams, as most HLS pieces are: 188-byte packets, each
// carrying a little of one stream, told apart by a number (PID). Table 0
// gives the PID of the program's map, which lists its streams and what
// each holds. The sound's packets, put together and with each PES header
// taken off, are the sound itself, ADTS AAC or MP3.

import (
	"errors"
	"fmt"
)

const tsPacket = 188

// tsDemuxer takes the sound out of transport stream pieces, remembering
// the PIDs from one piece to the next.
type tsDemuxer struct {
	pmt, audio  int // PIDs of the program map and of the sound, 0 until known
	codec       Codec
	unsupported string // the kind of sound found, if Mugig can't play it
}

func isTS(b []byte) bool {
	return len(b) >= tsPacket && b[0] == 0x47 && (len(b) < 2*tsPacket || b[tsPacket] == 0x47)
}

func (d *tsDemuxer) extract(b []byte) ([]byte, error) {
	var out []byte
	for i := 0; i+tsPacket <= len(b); {
		if b[i] != 0x47 { // out of step: find the next packet
			i++
			continue
		}
		p := b[i : i+tsPacket]
		i += tsPacket
		start := p[1]&0x40 != 0 // a table or PES packet begins here
		pid := int(p[1]&0x1F)<<8 | int(p[2])
		payload := p[4:]
		if p[3]&0x20 != 0 { // an adaptation field comes first
			n := int(payload[0])
			if 1+n > len(payload) {
				continue
			}
			payload = payload[1+n:]
		}
		if p[3]&0x10 == 0 { // no payload
			continue
		}
		switch {
		case pid == 0 && start:
			d.readPAT(payload)
		case pid == d.pmt && d.pmt != 0 && start:
			d.readPMT(payload)
		case pid == d.audio && d.audio != 0:
			if start {
				if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
					continue
				}
				header := 9 + int(payload[8])
				if header > len(payload) {
					continue
				}
				payload = payload[header:]
			}
			out = append(out, payload...)
		}
	}
	if d.audio == 0 {
		if d.unsupported != "" {
			return nil, fmt.Errorf("the station's sound is %s, which Mugig can't play", d.unsupported)
		}
		return nil, errors.New("no sound was found in the station's stream")
	}
	return out, nil
}

// section returns the table that begins in a packet's payload, after its
// pointer field, up to its checksum.
func section(payload []byte) []byte {
	if len(payload) < 1 || 1+int(payload[0]) > len(payload) {
		return nil
	}
	s := payload[1+int(payload[0]):]
	if len(s) < 3 {
		return nil
	}
	end := 3 + (int(s[1]&0x0F)<<8 | int(s[2])) - 4
	if end > len(s) || end < 8 {
		return nil
	}
	return s[:end]
}

// readPAT finds the first program's map.
func (d *tsDemuxer) readPAT(payload []byte) {
	s := section(payload)
	if s == nil || s[0] != 0 {
		return
	}
	for j := 8; j+4 <= len(s); j += 4 {
		if program := int(s[j])<<8 | int(s[j+1]); program != 0 {
			d.pmt = int(s[j+2]&0x1F)<<8 | int(s[j+3])
			return
		}
	}
}

// readPMT finds the program's first stream of sound Mugig can play.
func (d *tsDemuxer) readPMT(payload []byte) {
	s := section(payload)
	if s == nil || s[0] != 2 || len(s) < 12 {
		return
	}
	j := 12 + (int(s[10]&0x0F)<<8 | int(s[11]))
	for j+5 <= len(s) {
		kind, pid := s[j], int(s[j+1]&0x1F)<<8|int(s[j+2])
		j += 5 + (int(s[j+3]&0x0F)<<8 | int(s[j+4]))
		var codec Codec
		switch kind {
		case 0x0F:
			codec = AAC
		case 0x03, 0x04:
			codec = MP3
		case 0x11:
			d.unsupported = "AAC in LATM"
		case 0x81:
			d.unsupported = "AC-3"
		case 0x87:
			d.unsupported = "E-AC-3"
		}
		if codec != "" {
			d.audio, d.codec = pid, codec
			return
		}
	}
}
