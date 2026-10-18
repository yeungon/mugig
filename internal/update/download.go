package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxDownload is more than any download should be, so a bad answer can't
// fill the disk.
const maxDownload = 500 << 20

var downloader = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 30 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
}}

// Download fetches release r's download called asset, and its signature,
// into dir, and returns the download's path once its signature has been
// checked. progress, if not nil, is told how much has arrived, of total
// (-1 if unknown), now and then.
func Download(ctx context.Context, r Release, asset, dir string, progress func(done, total int64)) (string, error) {
	base, ok := strings.CutSuffix(r.Page, "/releases/tag/v"+r.Version)
	if !ok {
		return "", fmt.Errorf("unexpected release page %s", r.Page)
	}
	link := base + "/releases/download/v" + r.Version + "/" + asset
	sig, err := fetch(ctx, link+".sig", 4096, nil, nil)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, asset)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, err = fetch(ctx, link, maxDownload, f, progress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if err := verify(path, asset, r.Version, sig); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// fetch gets link, writing it to w, or returning it if w is nil, failing
// beyond limit bytes.
func fetch(ctx context.Context, link string, limit int64, w io.Writer, progress func(done, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloader.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: GitHub answered %s", filepath.Base(req.URL.Path), resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, errors.New(filepath.Base(req.URL.Path) + " is far bigger than it should be")
	}
	body := io.LimitReader(resp.Body, limit+1)
	if w == nil {
		b, err := io.ReadAll(body)
		if int64(len(b)) > limit {
			return nil, errors.New(filepath.Base(req.URL.Path) + " is far bigger than it should be")
		}
		return b, err
	}
	pw := &progressWriter{w: w, total: resp.ContentLength, report: progress}
	n, err := io.Copy(pw, body)
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errors.New(filepath.Base(req.URL.Path) + " is far bigger than it should be")
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	if progress != nil {
		progress(n, resp.ContentLength)
	}
	return nil, nil
}

// progressWriter tells report how much has been written, at most ten
// times a second; fetch tells it the total at the end.
type progressWriter struct {
	w      io.Writer
	done   int64
	total  int64
	last   time.Time
	report func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.report != nil && time.Since(p.last) > 100*time.Millisecond {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}
