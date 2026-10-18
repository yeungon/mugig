package update

// Releases are signed, so a copy of Mugig installs only what its author
// released: the release workflow signs each download with a private key
// kept as a GitHub secret (see tools/sign), and Mugig checks the signature
// with the public key below before installing anything. Someone who got
// into the GitHub account could replace the downloads and their checksums,
// but not sign them.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// publicKey checks releases' signatures; the private key it goes with
// signs them.
var publicKey = "JMmN8at6Lm5e4hwZ5uCInPyJAZlMa8cqqmGYW5HSM9Q="

// ErrBadSignature is returned for a download that isn't signed by the
// author, or was changed after it was.
var ErrBadSignature = errors.New("the download's signature doesn't match: it wasn't installed, as it may not come from Mugig's author")

// SignedMessage is what is signed for the file called name, as released
// for version, whose SHA-256 is sum: the name and version keep a signed
// file from passing for another system's or an older version's.
func SignedMessage(name, version string, sum []byte) []byte {
	return fmt.Appendf(nil, "mugig release\nfile %s\nversion %s\nsha256 %x\n", name, version, sum)
}

// FileSum is the SHA-256 of the file at path.
func FileSum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// verify checks that sig, as written in a .sig file, signs the file at
// path, released as name for version.
func verify(path, name, version string, sig []byte) error {
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("this copy of Mugig has no key to check updates with")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	sum, err := FileSum(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, SignedMessage(name, version, sum), raw) {
		return ErrBadSignature
	}
	return nil
}
