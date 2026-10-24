package store

import (
	"os"
	"testing"
)

// requireRealTrash skips tests that move folders into the user's real Recycle Bin or Trash
// unless GLINT_TEST_TRASH=1, so an ordinary test run leaves the bin alone.
func requireRealTrash(t *testing.T) {
	t.Helper()
	if os.Getenv("GLINT_TEST_TRASH") != "1" {
		t.Skip("set GLINT_TEST_TRASH=1 to test moving folders to the real Recycle Bin")
	}
}
