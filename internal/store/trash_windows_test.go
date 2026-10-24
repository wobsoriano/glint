package store

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// recycledPaths reads the original paths from the Recycle Bin's $I records for this user on
// the drive holding dir. A version 2 record is a header, size and time (24 bytes), a uint32
// length in UTF-16 units, then the path.
func recycledPaths(t *testing.T, dir string) []string {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(filepath.VolumeName(dir)+`\`, "$Recycle.Bin", user.User.Sid.String())
	records, _ := filepath.Glob(filepath.Join(bin, "$I*"))
	var paths []string
	for _, r := range records {
		b, err := os.ReadFile(r)
		if err != nil || len(b) < 28 || binary.LittleEndian.Uint64(b) != 2 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(b[24:]))
		u := make([]uint16, 0, n)
		for i := 28; i+1 < len(b) && len(u) < n; i += 2 {
			u = append(u, binary.LittleEndian.Uint16(b[i:]))
		}
		paths = append(paths, strings.TrimRight(string(utf16.Decode(u)), "\x00"))
	}
	return paths
}

func TestTrashLandsInTheRecycleBin(t *testing.T) {
	lib := New(t.TempDir())
	item, err := lib.ImportScreenshot(NewScreenshot{Base: checker(8, 8), Created: at(8, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Trash([]string{item.Dir}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(recycledPaths(t, item.Dir), item.Dir) {
		t.Errorf("%s is gone but not in the Recycle Bin", item.Dir)
	}
}
