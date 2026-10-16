// Package update finds out whether a newer Mugig has been released.
//
// GitHub sends a request for the newest release's page on to that
// release's own page, whose address ends in its tag, such as v0.2.0, so
// the redirect alone names the newest version: no API, with its limits,
// is needed. Pre-releases are never the newest.
package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LatestURL is the page GitHub sends on to the newest release.
var LatestURL = "https://github.com/yeungon/mugig/releases/latest"

// Release is a version of Mugig that has been released.
type Release struct {
	Version string // such as "0.2.0"
	Page    string // its release notes and downloads
}

// ErrNoRelease is returned when nothing has been released yet.
var ErrNoRelease = errors.New("no release yet")

var client = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // the redirect is the answer
	},
}

// Latest finds the newest release.
func Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, LatestURL, nil)
	if err != nil {
		return Release{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	page, err := resp.Location()
	if err != nil {
		return Release{}, err
	}
	return releaseAt(page)
}

// releaseAt reads the release a page is for, from its address, as in
// https://github.com/yeungon/mugig/releases/tag/v0.2.0. Without releases,
// GitHub sends on to the list of them, with no tag.
func releaseAt(page *url.URL) (Release, error) {
	dir, tag, _ := strings.Cut(page.Path, "/releases/tag/")
	if tag == "" || dir == "" || strings.Contains(tag, "/") {
		if strings.HasSuffix(strings.TrimSuffix(page.Path, "/"), "/releases") {
			return Release{}, ErrNoRelease
		}
		return Release{}, fmt.Errorf("GitHub sent an unexpected page: %s", page)
	}
	tag, err := url.PathUnescape(tag)
	if err != nil {
		return Release{}, err
	}
	v := strings.TrimPrefix(tag, "v")
	if _, ok := parse(v); !ok {
		return Release{}, fmt.Errorf("the newest release has an unexpected version: %q", tag)
	}
	return Release{Version: v, Page: page.String()}, nil
}

// Newer reports whether version v comes after version than, both of the
// form 1.2.3. Anything after a hyphen, as in 0.2.0-rc.1, is ignored, and a
// version that can't be read is never newer, nor older.
func Newer(v, than string) bool {
	a, okA := parse(v)
	b, okB := parse(than)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// parse reads a version of the form 1.2.3, or 1.2 or 1, the missing
// numbers being 0.
func parse(v string) ([3]int, bool) {
	var n [3]int
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || p == "" || p[0] == '+' {
			return n, false
		}
		n[i] = x
	}
	return n, true
}
