package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		v, than string
		want    bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.1.1", "0.1.0", true},
		{"0.2", "0.1.9", true},
		{"0.2.0", "0.2", false},
		{"0.2.0-rc.1", "0.1.0", true},
		{"0.2.0", "0.2.0-rc.1", false}, // a release candidate is built as its release
		{"0.2.0", "", false},
		{"", "0.1.0", false},
		{"0.2.x", "0.1.0", false},
		{"0.2.0.1", "0.1.0", false},
		{"+1.0.0", "0.1.0", false},
		{"-1.0.0", "0.1.0", false},
	} {
		if got := Newer(c.v, c.than); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.v, c.than, got)
		}
	}
}

func TestReleaseAt(t *testing.T) {
	for _, c := range []struct {
		page, version, err string
	}{
		{"https://github.com/yeungon/mugig/releases/tag/v0.2.0", "0.2.0", ""},
		{"https://github.com/yeungon/mugig/releases/tag/0.3.1", "0.3.1", ""},
		{"https://github.com/yeungon/mugig/releases", "", "no release yet"},
		{"https://github.com/yeungon/mugig/releases/", "", "no release yet"},
		{"https://github.com/login", "", "unexpected page"},
		{"https://github.com/yeungon/mugig/releases/tag/nightly", "", "unexpected version"},
		{"https://github.com/yeungon/mugig/releases/tag/v1/x", "", "unexpected page"},
	} {
		u, _ := url.Parse(c.page)
		r, err := releaseAt(u)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: %+v, %v; want error %q", c.page, r, err, c.err)
			}
			continue
		}
		if err != nil || r.Version != c.version || r.Page != c.page {
			t.Errorf("%s: %+v, %v", c.page, r, err)
		}
	}
}

// latestFrom points LatestURL at a server answering with h for the test.
func latestFrom(t *testing.T, h http.HandlerFunc) {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := LatestURL
	LatestURL = srv.URL + "/yeungon/mugig/releases/latest"
	t.Cleanup(func() { LatestURL = old })
}

// The newest release is read from where GitHub sends the request on to,
// without following it.
func TestLatest(t *testing.T) {
	var followed bool
	latestFrom(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yeungon/mugig/releases/latest":
			if r.Method != http.MethodHead {
				t.Errorf("asked with %s", r.Method)
			}
			http.Redirect(w, r, "/yeungon/mugig/releases/tag/v0.2.0", http.StatusFound)
		default:
			followed = true
		}
	})
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.2.0" || !strings.HasSuffix(r.Page, "/yeungon/mugig/releases/tag/v0.2.0") || !strings.HasPrefix(r.Page, "http://127.0.0.1") {
		t.Errorf("got %+v", r)
	}
	if followed {
		t.Error("followed the redirect")
	}
}

func TestLatestNoRelease(t *testing.T) {
	latestFrom(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/yeungon/mugig/releases", http.StatusFound)
	})
	if _, err := Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Errorf("got %v", err)
	}
}

func TestLatestFails(t *testing.T) {
	latestFrom(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	if _, err := Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("got %v", err)
	}
}
