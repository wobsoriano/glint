// Signswap is the windows.signCommand of a release build. SignPath signs files out of band, so
// the release workflow builds once, has SignPath sign Glint.exe, and builds again with this
// command. The second build compiles the same bytes, and signswap puts the signed copy in their
// place before mygo packs the installer.
//
//	signswap -unsigned <dir> -signed <dir> <file>
//
// A file whose bytes match a file in -unsigned is replaced by the file of the same name in
// -signed. Any other file, such as the installer itself, is left alone; the workflow signs that
// one afterwards.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	unsigned := flag.String("unsigned", os.Getenv("GLINT_UNSIGNED_DIR"), "folder of the files as submitted for signing")
	signed := flag.String("signed", os.Getenv("GLINT_SIGNED_DIR"), "folder of the same files as signed")
	flag.Parse()
	if flag.NArg() != 1 || *unsigned == "" || *signed == "" {
		fmt.Fprintln(os.Stderr, "usage: signswap -unsigned <dir> -signed <dir> <file>")
		os.Exit(2)
	}
	if err := swap(*unsigned, *signed, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "signswap:", err)
		os.Exit(1)
	}
}

func swap(unsignedDir, signedDir, file string) error {
	sum, err := hash(file)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(unsignedDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		s, err := hash(filepath.Join(unsignedDir, e.Name()))
		if err != nil {
			return err
		}
		if !bytes.Equal(s, sum) {
			continue
		}
		signed, err := os.ReadFile(filepath.Join(signedDir, e.Name()))
		if err != nil {
			return err
		}
		if signedSum := sha256.Sum256(signed); bytes.Equal(signedSum[:], sum) {
			return errors.New(e.Name() + " in the signed folder is not signed")
		}
		if err := os.WriteFile(file, signed, 0o755); err != nil {
			return err
		}
		fmt.Printf("signswap: %s is now the signed %s\n", filepath.Base(file), e.Name())
		return nil
	}
	fmt.Printf("signswap: %s matches nothing that was signed; left as built\n", filepath.Base(file))
	return nil
}

func hash(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := sha256.Sum256(b)
	return s[:], nil
}
