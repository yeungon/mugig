package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testKey makes a key pair for the test, putting its public key in
// publicKey, and returns a function that signs.
func testKey(t *testing.T) func(path, name, version string) []byte {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := publicKey
	publicKey = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { publicKey = old })
	return func(path, name, version string) []byte {
		sum, err := FileSum(path)
		if err != nil {
			t.Fatal(err)
		}
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, SignedMessage(name, version, sum))) + "\n")
	}
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerify(t *testing.T) {
	sign := testKey(t)
	path := writeFile(t, filepath.Join(t.TempDir(), "Mugig-macOS.zip"), "the new version")
	sig := sign(path, "Mugig-macOS.zip", "0.3.0")
	if err := verify(path, "Mugig-macOS.zip", "0.3.0", sig); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		name, version string
		sig           []byte
	}{
		"another system's":  {"Mugig-Windows.zip", "0.3.0", sig},
		"another version's": {"Mugig-macOS.zip", "0.2.0", sig},
		"not base64":        {"Mugig-macOS.zip", "0.3.0", []byte("hello")},
		"too short":         {"Mugig-macOS.zip", "0.3.0", []byte("aGVsbG8=")},
	} {
		if err := verify(path, c.name, c.version, c.sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
	writeFile(t, path, "the new version, changed")
	if err := verify(path, "Mugig-macOS.zip", "0.3.0", sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("changed file: %v", err)
	}
}

// Signed with another key, a file doesn't pass.
func TestVerifyOtherKey(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "Mugig-macOS.zip"), "the new version")
	sig := testKey(t)(path, "Mugig-macOS.zip", "0.3.0")
	testKey(t)
	if err := verify(path, "Mugig-macOS.zip", "0.3.0", sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("got %v", err)
	}
}

// The key built in is one.
func TestPublicKey(t *testing.T) {
	if b, err := base64.StdEncoding.DecodeString(publicKey); err != nil || len(b) != ed25519.PublicKeySize {
		t.Errorf("publicKey %q: %v", publicKey, err)
	}
}
