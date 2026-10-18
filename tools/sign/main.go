// Command sign signs Mugig's downloads for a release, so copies of Mugig
// install only what was signed with the private key (see
// internal/update/verify.go). The release workflow runs it:
//
//	MUGIG_SIGNING_KEY=… go run ./tools/sign -version 0.3.0 out/Mugig-*
//
// writing a .sig file next to each. The key is the base64 seed of an
// Ed25519 key, kept as the repository's MUGIG_SIGNING_KEY secret.
//
// To make a new pair of keys, writing the private one to a file only you
// can read and printing the public one, for internal/update's publicKey:
//
//	go run ./tools/sign -new-key ~/.mugig-signing-key
//
// Copies of Mugig only accept updates signed with the private key that
// goes with their public one, so a new pair means every copy before it
// has to be updated by hand once.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"mugig/internal/update"
)

func main() {
	log.SetFlags(0)
	version := flag.String("version", "", "the version released, as in 0.3.0")
	newKey := flag.String("new-key", "", "make a new pair of keys, writing the private one to this file")
	flag.Parse()
	if *newKey != "" {
		if err := makeKeys(*newKey); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *version == "" || flag.NArg() == 0 {
		log.Fatal("usage: sign -version 0.3.0 file…")
	}
	key, err := privateKey(os.Getenv("MUGIG_SIGNING_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	for _, path := range flag.Args() {
		if err := sign(key, path, *version); err != nil {
			log.Fatal(err)
		}
		fmt.Println("signed", path)
	}
}

func privateKey(seed string) (ed25519.PrivateKey, error) {
	if seed == "" {
		return nil, errors.New("MUGIG_SIGNING_KEY isn't set: add the private key as a repository secret of that name")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seed))
	if err != nil || len(b) != ed25519.SeedSize {
		return nil, errors.New("MUGIG_SIGNING_KEY isn't a key made by sign -new-key")
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// sign writes path.sig, signing the file at path as released for version.
func sign(key ed25519.PrivateKey, path, version string) error {
	sum, err := update.FileSum(path)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(key, update.SignedMessage(filepath.Base(path), version, sum))
	return os.WriteFile(path+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

func makeKeys(path string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	seed := base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // never over an existing key
	if err != nil {
		return err
	}
	if _, err := f.WriteString(seed); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Private key written to %s\nPublic key: %s\n", path, base64.StdEncoding.EncodeToString(pub))
	return nil
}
