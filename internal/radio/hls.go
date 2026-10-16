package radio

// HLS, HTTP Live Streaming. A station's .m3u8 playlist lists the latest
// few pieces of its broadcast, each a few seconds long, and grows as new
// ones are made; older ones fall off the top. Playing it means fetching
// the pieces in turn and fetching the playlist again for more. A master
// playlist lists the same broadcast at several bitrates instead, each its
// own playlist.
//
// Pieces are fetched a little ahead of playing, so a slow fetch doesn't
// break up the sound.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// liveEdge is how many pieces from the newest a live broadcast starts,
	// as the HLS specification asks, so there is sound in hand.
	liveEdge = 3
	// piecesAhead is how many pieces are fetched ahead of playing.
	piecesAhead = 2
	// maxPiece is the most of a piece read.
	maxPiece = 64 << 20
	// fetchTries is how many times a piece or playlist is asked for before
	// giving up.
	fetchTries = 3
)

// mediaPlaylist is a playlist of pieces.
type mediaPlaylist struct {
	target    time.Duration // the longest a piece lasts, and so how often to look for more
	seq       int64         // the sequence number of the first piece listed
	pieces    []string      // links to the pieces
	lengths   []float64     // each piece's length in seconds, 0 if not given
	ended     bool          // no more will be added: not live
	initLink  string        // the initialization section that fragmented MP4 pieces need
	encrypted bool
}

// masterPlaylist lists versions of the same broadcast.
type masterPlaylist struct {
	variants []variant
	audio    []rendition // separate sound for the variants, by group
}

type variant struct {
	link       string
	bandwidth  int // bits a second
	codecs     string
	audioGroup string // its sound is in this group of renditions
}

type rendition struct {
	group, link string
	isDefault   bool
}

// parseHLS reads an HLS playlist found at base: a master playlist, or one
// of pieces.
func parseHLS(text string, base *url.URL) (*masterPlaylist, *mediaPlaylist) {
	master, media := &masterPlaylist{}, &mediaPlaylist{}
	var pending *variant
	length := 0.0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		tag, value, _ := strings.Cut(line, ":")
		switch {
		case tag == "#EXT-X-STREAM-INF":
			a := attributes(value)
			bw, _ := strconv.Atoi(a["BANDWIDTH"])
			pending = &variant{bandwidth: bw, codecs: a["CODECS"], audioGroup: a["AUDIO"]}
		case tag == "#EXT-X-MEDIA":
			if a := attributes(value); a["TYPE"] == "AUDIO" && a["URI"] != "" {
				master.audio = append(master.audio, rendition{group: a["GROUP-ID"], link: resolve(base, a["URI"]), isDefault: a["DEFAULT"] == "YES"})
			}
		case tag == "#EXT-X-TARGETDURATION":
			if s, err := strconv.ParseFloat(value, 64); err == nil {
				media.target = time.Duration(s * float64(time.Second))
			}
		case tag == "#EXTINF":
			length, _ = strconv.ParseFloat(strings.TrimSpace(strings.Split(value, ",")[0]), 64)
		case tag == "#EXT-X-MEDIA-SEQUENCE":
			media.seq, _ = strconv.ParseInt(value, 10, 64)
		case tag == "#EXT-X-ENDLIST":
			media.ended = true
		case tag == "#EXT-X-MAP":
			media.initLink = resolve(base, attributes(value)["URI"])
		case tag == "#EXT-X-KEY":
			if m := attributes(value)["METHOD"]; m != "" && m != "NONE" {
				media.encrypted = true
			}
		case line == "" || strings.HasPrefix(line, "#"):
		case pending != nil:
			pending.link = resolve(base, line)
			master.variants = append(master.variants, *pending)
			pending = nil
		default:
			media.pieces = append(media.pieces, resolve(base, line))
			media.lengths = append(media.lengths, length)
			length = 0
		}
	}
	if len(master.variants) > 0 {
		return master, nil
	}
	if media.target <= 0 {
		media.target = 6 * time.Second
	}
	return nil, media
}

// attributes reads an HLS tag's list, such as BANDWIDTH=96000,CODECS="mp4a.40.5".
func attributes(s string) map[string]string {
	m := map[string]string{}
	for s != "" {
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		var v string
		if strings.HasPrefix(rest, `"`) {
			v, rest, _ = strings.Cut(rest[1:], `"`)
			_, rest, _ = strings.Cut(rest, ",")
		} else {
			v, rest, _ = strings.Cut(rest, ",")
		}
		m[strings.ToUpper(strings.TrimSpace(k))] = v
		s = rest
	}
	return m
}

// choose picks the version to play, and its bitrate: the best sounding
// that is sound alone, or failing that, the smallest video, whose sound
// is taken out of it.
func (m *masterPlaylist) choose() (link string, bandwidth int) {
	video := func(v variant) bool {
		c := strings.ToLower(v.codecs)
		for _, x := range []string{"avc", "hvc", "hev", "vp0", "av01", "dvh"} {
			if strings.Contains(c, x) {
				return true
			}
		}
		return false
	}
	var best *variant
	for i, v := range m.variants {
		if !video(v) && (best == nil || v.bandwidth > best.bandwidth) {
			best = &m.variants[i]
		}
	}
	if best == nil {
		for i, v := range m.variants {
			if best == nil || v.bandwidth < best.bandwidth {
				best = &m.variants[i]
			}
		}
	}
	// Sound kept separately from the video is fetched on its own.
	if best.audioGroup != "" {
		var pick *rendition
		for i, r := range m.audio {
			if r.group == best.audioGroup && (pick == nil || r.isDefault && !pick.isDefault) {
				pick = &m.audio[i]
			}
		}
		if pick != nil {
			return pick.link, best.bandwidth
		}
	}
	return best.link, best.bandwidth
}

// hls fetches an HLS broadcast's pieces, taking the sound out of each.
type hls struct {
	ctx     context.Context
	link    string // the playlist of pieces
	pl      *mediaPlaylist
	loaded  time.Time // when pl was fetched
	lastNew time.Time // when pl last brought new pieces
	next    int64     // the sequence number of the next piece to fetch
	demux   demuxer

	pieces chan []byte // sound, fetched ahead; closed after err is set
	err    error
	cur    []byte // the piece being read
	cancel context.CancelFunc
}

// openHLS starts playing the HLS playlist text, found at base.
func openHLS(ctx context.Context, base *url.URL, text string) (*Stream, error) {
	ctx, cancel := context.WithCancel(ctx)
	s, err := startHLS(ctx, base, text)
	if err != nil {
		cancel()
		return nil, err
	}
	s.ReadCloser.(*hls).cancel = cancel
	return s, nil
}

func startHLS(ctx context.Context, base *url.URL, text string) (*Stream, error) {
	master, pl := parseHLS(text, base)
	kbps := 0
	if master != nil {
		link, bandwidth := master.choose()
		kbps = bandwidth / 1000
		var err error
		if text, base, err = fetchText(ctx, link); err != nil {
			return nil, err
		}
		if master, pl = parseHLS(text, base); master != nil {
			return nil, errors.New("the station's playlist lists playlists that list playlists")
		}
	}
	if pl.encrypted {
		return nil, errors.New("the station's stream is encrypted, which Mugig can't play")
	}
	if pl.ended && len(pl.pieces) == 0 {
		return nil, errNoStream
	}
	now := time.Now()
	h := &hls{ctx: ctx, link: base.String(), pl: pl, loaded: now, lastNew: now, next: pl.seq, pieces: make(chan []byte, piecesAhead)}
	h.demux.fetchInit = func(link string) ([]byte, error) { return h.fetch(link) }
	if !pl.ended {
		h.next += int64(max(len(pl.pieces)-liveEdge, 0))
	}
	// The first piece is fetched now: it says how the sound is encoded,
	// and if the playlist didn't, its bitrate.
	i := h.next - pl.seq
	first, err := h.piece()
	if err != nil {
		return nil, err
	}
	h.cur = first
	if kbps == 0 && i >= 0 && i < int64(len(pl.lengths)) && pl.lengths[i] > 0 {
		kbps = roundKbps(float64(len(first)) * 8 / pl.lengths[i] / 1000)
	}
	go h.fetchAhead()
	return &Stream{ReadCloser: h, Codec: h.demux.codec, HLS: true, Kbps: kbps}, nil
}

func (h *hls) Read(p []byte) (int, error) {
	for len(h.cur) == 0 {
		b, ok := <-h.pieces
		if !ok {
			return 0, h.err
		}
		h.cur = b
	}
	n := copy(p, h.cur)
	h.cur = h.cur[n:]
	return n, nil
}

func (h *hls) Close() error {
	h.cancel()
	return nil
}

// roundKbps rounds a bitrate worked out from a piece's size to the usual
// bitrate it is near, as encoders are set to those.
func roundKbps(kbps float64) int {
	for _, k := range []float64{24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320} {
		if math.Abs(kbps-k) <= k*0.08 {
			return int(k)
		}
	}
	return int(math.Round(kbps))
}

// fetchAhead fetches pieces until the broadcast ends, fails, or is hung up.
func (h *hls) fetchAhead() {
	defer close(h.pieces)
	for {
		b, err := h.piece()
		if err != nil {
			h.err = err
			if h.ctx.Err() != nil {
				h.err = io.ErrClosedPipe
			}
			return
		}
		select {
		case h.pieces <- b:
		case <-h.ctx.Done():
			h.err = io.ErrClosedPipe
			return
		}
	}
}

// piece fetches the next piece's sound, waiting for the playlist to grow
// if need be. It returns io.EOF at the end of a broadcast that has one.
func (h *hls) piece() ([]byte, error) {
	for {
		if i := h.next - h.pl.seq; i >= 0 && i < int64(len(h.pl.pieces)) {
			h.next++
			data, err := h.fetch(h.pl.pieces[i])
			var gone *statusError
			if errors.As(err, &gone) && gone.missing() {
				continue // it fell off the playlist meanwhile
			}
			if err != nil {
				return nil, err
			}
			sound, err := h.demux.extract(data, h.pl.initLink)
			if err != nil {
				return nil, err
			}
			if len(sound) > 0 {
				return sound, nil
			}
			continue
		}
		if h.next < h.pl.seq {
			h.next = h.pl.seq // fell behind: what was missed is gone
			continue
		}
		if h.pl.ended {
			return nil, io.EOF
		}
		if err := h.reload(); err != nil {
			return nil, err
		}
	}
}

// reload waits until the playlist is due to have grown, and fetches it
// again. The wait is a piece's length, or half that after a fetch that
// brought nothing new, as the HLS specification asks.
func (h *hls) reload() error {
	wait := h.pl.target
	if !h.lastNew.Equal(h.loaded) {
		wait /= 2
	}
	if time.Since(h.lastNew) > max(3*h.pl.target, stallTimeout) {
		return errStalled
	}
	t := time.NewTimer(time.Until(h.loaded.Add(wait)))
	select {
	case <-t.C:
	case <-h.ctx.Done():
		t.Stop()
		return h.ctx.Err()
	}
	var text string
	var base *url.URL
	var err error
	for try := range fetchTries {
		if text, base, err = fetchText(h.ctx, h.link); err == nil || h.ctx.Err() != nil {
			break
		}
		sleep(h.ctx, time.Duration(try+1)*time.Second)
	}
	if err != nil {
		return err
	}
	_, pl := parseHLS(text, base)
	if pl == nil {
		return errors.New("the station's playlist changed into another kind")
	}
	h.loaded = time.Now()
	if pl.seq+int64(len(pl.pieces)) > h.next || pl.ended {
		h.lastNew = h.loaded
	}
	h.pl = pl
	return nil
}

// fetch reads the piece at link, asking again after a failure.
func (h *hls) fetch(link string) ([]byte, error) {
	var err error
	for try := range fetchTries {
		var resp *http.Response
		if resp, err = get(h.ctx, link, false); err == nil {
			var b []byte
			b, err = io.ReadAll(io.LimitReader(resp.Body, maxPiece))
			resp.Body.Close()
			if err == nil {
				return b, nil
			}
		}
		var status *statusError
		if h.ctx.Err() != nil || errors.As(err, &status) && status.missing() {
			break
		}
		sleep(h.ctx, time.Duration(try+1)*500*time.Millisecond)
	}
	return nil, err
}

// fetchText reads the playlist at link, returning where it was found
// after any redirects.
func fetchText(ctx context.Context, link string) (string, *url.URL, error) {
	resp, err := get(ctx, link, false)
	if err != nil {
		return "", nil, err
	}
	text, err := readPlaylist(resp.Body, resp.Body)
	return text, resp.Request.URL, err
}

// sleep waits for d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// demuxer takes the sound out of HLS pieces, of whatever kind.
type demuxer struct {
	codec     Codec
	ts        tsDemuxer
	mp4       *fmp4
	fetchInit func(link string) ([]byte, error)
}

func (d *demuxer) extract(piece []byte, initLink string) ([]byte, error) {
	var sound []byte
	var codec Codec
	var err error
	switch {
	case isTS(piece):
		sound, err = d.ts.extract(piece)
		codec = d.ts.codec
	case initLink != "" || isMP4(piece):
		if d.mp4 == nil {
			if initLink == "" {
				return nil, errors.New("the station's MP4 pieces come without their initialization section")
			}
			init, err := d.fetchInit(initLink)
			if err != nil {
				return nil, err
			}
			if d.mp4, err = parseInit(init); err != nil {
				return nil, err
			}
		}
		sound, err = d.mp4.extract(piece)
		codec = d.mp4.codec
	default: // packed sound: AAC or MP3 as it is, after an ID3 tag
		sound = skipID3(piece)
		var ok bool
		if codec, ok = sniffCodec(sound); !ok || codec == Ogg {
			return nil, errors.New("the station's pieces are of a kind Mugig can't play")
		}
	}
	if err != nil {
		return nil, err
	}
	if d.codec == "" {
		d.codec = codec
	} else if codec != d.codec {
		return nil, fmt.Errorf("the station changed from %s to %s", d.codec, codec)
	}
	return sound, nil
}

// skipID3 skips the ID3 tags at the start of b, which packed HLS sound
// begins with to say when it was made.
func skipID3(b []byte) []byte {
	for len(b) >= 10 && string(b[:3]) == "ID3" {
		n := 10 + (int(b[6]&0x7F)<<21 | int(b[7]&0x7F)<<14 | int(b[8]&0x7F)<<7 | int(b[9]&0x7F))
		if b[5]&0x10 != 0 { // a footer
			n += 10
		}
		if n > len(b) {
			return nil
		}
		b = b[n:]
	}
	return b
}

// statusError is a station's answer other than OK.
type statusError struct {
	status string
	code   int
}

func (e *statusError) Error() string { return fmt.Sprintf("the station answered %q", e.status) }

// missing reports whether what was asked for isn't there (any more).
func (e *statusError) missing() bool {
	return e.code == http.StatusNotFound || e.code == http.StatusGone
}
