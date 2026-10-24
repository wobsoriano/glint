package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapReplacesOnlyTheFileThatWasSigned(t *testing.T) {
	dir := t.TempDir()
	write := func(path, body string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	unsigned, signed := filepath.Join(dir, "unsigned"), filepath.Join(dir, "signed")
	write(filepath.Join(unsigned, "Glint.exe"), "app")
	write(filepath.Join(signed, "Glint.exe"), "app+signature")

	rebuilt := write(filepath.Join(dir, "stage", "Glint.exe"), "app")
	if err := swap(unsigned, signed, rebuilt); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rebuilt); string(b) != "app+signature" {
		t.Errorf("the rebuilt Glint.exe holds %q, want the signed copy", b)
	}

	setup := write(filepath.Join(dir, "Glint Setup.exe"), "installer")
	if err := swap(unsigned, signed, setup); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(setup); string(b) != "installer" {
		t.Errorf("the installer changed to %q", b)
	}

	write(filepath.Join(signed, "Glint.exe"), "app")
	if err := swap(unsigned, signed, write(filepath.Join(dir, "again", "Glint.exe"), "app")); err == nil {
		t.Error("an unsigned copy in the signed folder should fail the build")
	}
}
