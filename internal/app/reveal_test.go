package app

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestByFolderKeepsEveryPathUnderItsFolder(t *testing.T) {
	lib, other := filepath.Join("C:", "Glint", "Library"), filepath.Join("D:", "Shots")
	a, b, c := filepath.Join(lib, "a"), filepath.Join(other, "b"), filepath.Join(lib, "c")
	got := byFolder([]string{a, b, c})
	want := []folder{{lib, []string{a, c}}, {other, []string{b}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("byFolder: %+v, want %+v", got, want)
	}
}
