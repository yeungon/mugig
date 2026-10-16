package radio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Codec is how a stream's sound is encoded.
type Codec string

const (
	MP3 Codec = "MP3"
	AAC Codec = "AAC"
	Ogg Codec = "Ogg Vorbis"
)

// Stream is a station's sound as it arrives, still encoded. Reading waits
// for the station; Close hangs up.
type Stream struct {
	io.ReadCloser
	Codec Codec
	HLS   bool   // fetched piece by piece from an HLS playlist
	Kbps  int    // the bitrate the station states, 0 if it doesn't
	Name  string // the station's name for itself, "" if it gives none
}

const (
	// connectTimeout is how long a station has to answer.
	connectTimeout = 15 * time.Second
	// maxPlaylist is the most of a playlist read: they are a few lines.
	maxPlaylist = 1 << 20
	// maxDepth is how many playlists can lead to the next.
	maxDepth = 5
)

// stallTimeout is how long a station can go without sending anything
// before the connection is given up. Tests shorten it.
var stallTimeout = 20 * time.Second

// userAgent names Mugig to stations; a few refuse requests without one.
const userAgent = "Mugig/1.0 (+https://github.com/yeungon/mugig)"

var (
	errWebPage  = errors.New("the link is to a web page, not a radio stream. The page may have a link to the stream itself, often ending .m3u8, .pls, .m3u or .mp3")
	errStalled  = errors.New("the station stopped sending")
	errNoStream = errors.New("the playlist lists no streams")
)

var client = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           dialICY,
	TLSHandshakeTimeout:   connectTimeout,
	ResponseHeaderTimeout: connectTimeout,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   4,
	ForceAttemptHTTP2:     true,
}}

// Open connects to the station at link and returns its sound. It waits
// for the station to answer, following playlists to the stream itself.
// onTitle, if not nil, is called with what is playing whenever the
// station names it, on the goroutine reading the stream. Cancelling ctx
// hangs up.
func Open(ctx context.Context, link string, onTitle func(string)) (*Stream, error) {
	if onTitle == nil {
		onTitle = func(string) {}
	}
	return open(ctx, link, onTitle, 0)
}

func open(ctx context.Context, link string, onTitle func(string), depth int) (*Stream, error) {
	if depth > maxDepth {
		return nil, errors.New("the link leads through too many playlists")
	}
	u, err := ParseLink(link)
	if err != nil {
		return nil, err
	}
	resp, err := get(ctx, u.String(), true)
	if err != nil {
		return nil, err
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	ct = strings.ToLower(ct)
	br := bufio.NewReaderSize(resp.Body, 32<<10)
	head, _ := br.Peek(512)  // as much as comes, if less
	base := resp.Request.URL // after any redirects

	switch {
	case isHTML(ct, head):
		resp.Body.Close()
		return nil, errWebPage
	case isPLS(ct, head):
		text, err := readPlaylist(br, resp.Body)
		if err != nil {
			return nil, err
		}
		return openFirst(ctx, parsePLS(text, base), onTitle, depth)
	case isM3U(ct, head, base.Path):
		text, err := readPlaylist(br, resp.Body)
		if err != nil {
			return nil, err
		}
		if strings.Contains(text, "#EXT-X-") {
			return openHLS(ctx, base, text)
		}
		return openFirst(ctx, parseM3U(text, base), onTitle, depth)
	}
	codec, err := streamCodec(ct, head)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	var r io.Reader = br
	if every, _ := strconv.Atoi(resp.Header.Get("icy-metaint")); every > 0 {
		r = &icyReader{r: br, every: every, left: every, onTitle: onTitle}
	}
	return &Stream{
		ReadCloser: struct {
			io.Reader
			io.Closer
		}{r, resp.Body},
		Codec: codec,
		Kbps:  leadingInt(resp.Header.Get("icy-br")),
		Name:  strings.TrimSpace(resp.Header.Get("icy-name")),
	}, nil
}

// ParseLink checks that link is a web address a station could be at.
func ParseLink(link string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q isn't a web link: it should start with http:// or https://", link)
	}
	return u, nil
}

// openFirst opens the first of links that plays.
func openFirst(ctx context.Context, links []string, onTitle func(string), depth int) (*Stream, error) {
	err := errNoStream
	for _, l := range links {
		var s *Stream
		if s, err = open(ctx, l, onTitle, depth+1); err == nil {
			return s, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}

// get requests link. The answer must begin within connectTimeout, and its
// body is given up if nothing comes for stallTimeout. Closing the body
// hangs up.
func get(parent context.Context, link string, icy bool) (*http.Response, error) {
	ctx, cancel := context.WithCancel(parent)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if icy {
		req.Header.Set("Icy-MetaData", "1") // ask for the titles of what plays
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, reachError(req.URL.Hostname(), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, &statusError{resp.Status, resp.StatusCode}
	}
	resp.Body = newWatchdog(resp.Body, cancel)
	return resp, nil
}

// reachError says why a station couldn't be reached, in plain words where
// the reason is a common one.
func reachError(host string, err error) error {
	var dns *net.DNSError
	var ne net.Error
	var errno syscall.Errno
	switch {
	case errors.As(err, &dns):
		return fmt.Errorf("couldn't find %s: check the link, and that you're online", host)
	case errors.As(err, &errno) && (errno == syscall.ECONNREFUSED || errno == 10061): // 10061: Windows' own number for it
		return fmt.Errorf("%s refused the connection: the station may be off the air", host)
	case errors.As(err, &ne) && ne.Timeout():
		return fmt.Errorf("%s took too long to answer", host)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("couldn't reach the station: %w", err)
}

// watchdog hangs up if the body stops arriving for stallTimeout.
type watchdog struct {
	body    io.ReadCloser
	cancel  func()
	timer   *time.Timer
	stalled atomic.Bool
}

func newWatchdog(body io.ReadCloser, cancel func()) *watchdog {
	w := &watchdog{body: body, cancel: cancel}
	w.timer = time.AfterFunc(stallTimeout, func() {
		w.stalled.Store(true)
		cancel()
	})
	return w
}

func (w *watchdog) Read(p []byte) (int, error) {
	n, err := w.body.Read(p)
	if n > 0 {
		w.timer.Reset(stallTimeout)
	}
	if err != nil && err != io.EOF && w.stalled.Load() {
		err = errStalled
	}
	return n, err
}

func (w *watchdog) Close() error {
	w.timer.Stop()
	w.cancel()
	return w.body.Close()
}

// readPlaylist reads a playlist's text, and hangs up.
func readPlaylist(r io.Reader, body io.Closer) (string, error) {
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxPlaylist))
	if err != nil {
		return "", fmt.Errorf("couldn't read the playlist: %w", err)
	}
	return string(b), nil
}

func isHTML(ct string, head []byte) bool {
	if ct == "text/html" || ct == "application/xhtml+xml" {
		return true
	}
	h := bytes.ToLower(bytes.TrimSpace(head[:min(len(head), 64)]))
	return bytes.HasPrefix(h, []byte("<!doctype html")) || bytes.HasPrefix(h, []byte("<html"))
}

func isPLS(ct string, head []byte) bool {
	return ct == "audio/x-scpls" || ct == "audio/scpls" ||
		bytes.HasPrefix(bytes.ToLower(bytes.TrimSpace(head)), []byte("[playlist]"))
}

// isM3U reports whether a link's answer is an M3U playlist, plain or HLS.
func isM3U(ct string, head []byte, urlPath string) bool {
	switch ct {
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return true
	}
	h := bytes.TrimSpace(bytes.TrimPrefix(head, []byte("\xEF\xBB\xBF")))
	if bytes.HasPrefix(h, []byte("#EXTM3U")) {
		return true
	}
	ext := strings.ToLower(path.Ext(urlPath))
	return (ext == ".m3u" || ext == ".m3u8") && (bytes.HasPrefix(h, []byte("http")) || bytes.HasPrefix(h, []byte("#")))
}

// parsePLS lists a .pls playlist's links: its File1=, File2= lines.
func parsePLS(text string, base *url.URL) []string {
	var links []string
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.HasPrefix(strings.ToLower(k), "file") {
			if l := resolve(base, v); l != "" {
				links = append(links, l)
			}
		}
	}
	return links
}

// parseM3U lists a plain M3U playlist's links: its lines that aren't
// comments.
func parseM3U(text string, base *url.URL) []string {
	var links []string
	for _, line := range strings.Split(strings.TrimPrefix(text, "\xEF\xBB\xBF"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if l := resolve(base, line); l != "" {
				links = append(links, l)
			}
		}
	}
	return links
}

// resolve turns ref, found in a playlist at base, into a whole link.
func resolve(base *url.URL, ref string) string {
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ""
	}
	return base.ResolveReference(r).String()
}

// streamCodec finds how a stream is encoded: from what the station says,
// or failing that, from its first bytes.
func streamCodec(ct string, head []byte) (Codec, error) {
	switch ct {
	case "audio/mpeg", "audio/mp3", "audio/mpeg3", "audio/x-mpeg", "audio/mpg":
		return MP3, nil
	case "audio/aac", "audio/aacp", "audio/x-aac", "audio/x-aacp", "audio/aac-adts":
		return AAC, nil
	}
	if c, ok := sniffCodec(head); ok {
		if c == Ogg {
			return Ogg, oggCodecError(head)
		}
		return c, nil
	}
	if ct == "" {
		ct = "of an unknown kind"
	}
	return "", fmt.Errorf("Mugig can't play this stream (%s)", ct)
}

// sniffCodec finds how sound is encoded from its first bytes.
func sniffCodec(b []byte) (Codec, bool) {
	switch {
	case bytes.HasPrefix(b, []byte("OggS")):
		return Ogg, true
	case bytes.HasPrefix(b, []byte("ID3")):
		return MP3, true // MP3 most often follows a tag
	}
	for i := 0; i+1 < len(b); i++ {
		if b[i] != 0xFF || b[i+1]&0xE0 != 0xE0 {
			continue
		}
		if b[i+1]&0xF6 == 0xF0 { // ADTS: a 12-bit sync and layer 0
			return AAC, true
		}
		if b[i+1]&0x06 != 0 { // MPEG audio: an 11-bit sync and a layer
			return MP3, true
		}
	}
	return "", false
}

// oggCodecError says if an Ogg stream holds something other than Vorbis.
func oggCodecError(head []byte) error {
	switch {
	case bytes.Contains(head, []byte("OpusHead")):
		return errors.New("Mugig can't play Ogg Opus streams yet")
	case bytes.Contains(head, []byte("\x7FFLAC")):
		return errors.New("Mugig can't play Ogg FLAC streams yet")
	}
	return nil
}

// leadingInt reads the number s starts with, as in "128" or "128,128".
func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// dialICY connects as usual, but lets old Shoutcast servers, which begin
// their answer "ICY 200 OK" rather than "HTTP/1.0 200 OK", be understood.
func dialICY(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &icyConn{Conn: c}, nil
}

// icyConn rewrites a Shoutcast answer's first word. Encrypted connections
// never begin with "ICY ", so those pass as they are.
type icyConn struct {
	net.Conn
	checked bool
	pending []byte
}

func (c *icyConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	n, err := c.Conn.Read(p)
	if !c.checked && n > 0 {
		c.checked = true
		if n >= 4 && string(p[:4]) == "ICY " {
			fixed := append([]byte("HTTP/1.0 "), p[4:n]...)
			n = copy(p, fixed)
			c.pending = fixed[n:]
		}
	}
	return n, err
}
