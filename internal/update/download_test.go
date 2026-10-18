package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeRelease serves release 0.3.0's download and signature as GitHub
// does, returning the release.
func fakeRelease(t *testing.T, body string, sig func(data []byte) []byte) Release {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/yeungon/mugig/releases/download/v0.3.0/Mugig-macOS.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write([]byte(body))
	})
	mux.HandleFunc("/yeungon/mugig/releases/download/v0.3.0/Mugig-macOS.zip.sig", func(w http.ResponseWriter, r *http.Request) {
		if sig == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(sig([]byte(body)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Release{Version: "0.3.0", Page: srv.URL + "/yeungon/mugig/releases/tag/v0.3.0"}
}

// signer signs data as the download named, for 0.3.0.
func signer(t *testing.T) func([]byte) []byte {
	sign := testKey(t)
	return func(data []byte) []byte {
		path := writeFile(t, filepath.Join(t.TempDir(), "f"), string(data))
		return sign(path, "Mugig-macOS.zip", "0.3.0")
	}
}

func TestDownload(t *testing.T) {
	body := strings.Repeat("new version ", 100000)
	r := fakeRelease(t, body, signer(t))
	var last, total int64
	path, err := Download(context.Background(), r, "Mugig-macOS.zip", t.TempDir(), func(d, n int64) { last, total = d, n })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != body || filepath.Base(path) != "Mugig-macOS.zip" {
		t.Errorf("downloaded %d bytes to %s", len(b), path)
	}
	if last != int64(len(body)) || total != int64(len(body)) {
		t.Errorf("progress %d of %d", last, total)
	}
}

// A download whose signature doesn't match is deleted.
func TestDownloadBadSignature(t *testing.T) {
	sign := signer(t)
	r := fakeRelease(t, "tampered", func([]byte) []byte { return sign([]byte("the real one")) })
	dir := t.TempDir()
	if _, err := Download(context.Background(), r, "Mugig-macOS.zip", dir, nil); !errors.Is(err, ErrBadSignature) {
		t.Errorf("got %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

// Without a signature, nothing is downloaded.
func TestDownloadUnsigned(t *testing.T) {
	testKey(t)
	r := fakeRelease(t, "unsigned", nil)
	dir := t.TempDir()
	if _, err := Download(context.Background(), r, "Mugig-macOS.zip", dir, nil); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("got %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestDownloadCancelled(t *testing.T) {
	r := fakeRelease(t, "x", signer(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Download(ctx, r, "Mugig-macOS.zip", t.TempDir(), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}
