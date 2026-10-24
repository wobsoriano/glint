package app

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The registrations are written under a scratch key of HKEY_CURRENT_USER, laid out as the
// Rust build lays them out under Software\Classes, so the user's own classes stay untouched.
func TestRemoveRustOpenWithKeepsOtherHandlers(t *testing.T) {
	root := fmt.Sprintf(`Software\GlintTest-%d`, os.Getpid())
	t.Cleanup(func() { deleteTree(root) })
	set := func(path, name, value string) {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\`+path, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		defer k.Close()
		if err := k.SetStringValue(name, value); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(path string) bool {
		k, err := registry.OpenKey(registry.CURRENT_USER, root+`\`+path, registry.QUERY_VALUE)
		if err != nil {
			return false
		}
		k.Close()
		return true
	}
	values := func(path string) []string {
		k, err := registry.OpenKey(registry.CURRENT_USER, root+`\`+path, registry.QUERY_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		defer k.Close()
		names, err := k.ReadValueNames(0)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(names)
		return names
	}

	old := `"C:\Users\me\Downloads\glint.exe" "%1"`
	set(`Glint.Image`, "", "Glint Image")
	set(`Glint.Image\shell\open\command`, "", old)
	set(`Glint.Video\shell\open\command`, "", `"C:\Users\me\Downloads\glint.exe" --trim "%1"`)
	set(`Applications\glint.exe\shell\open\command`, "", old)
	set(`.png\OpenWithProgids`, "Glint.Image", "")
	set(`.png\OpenWithProgids`, "com.glint.app.png", "")
	set(`.png\OpenWithProgids`, "AppXphotos", "")
	set(`.mp4\OpenWithProgids`, "Glint.Video", "")

	removed := removeRustOpenWith(root, `C:\Users\me\AppData\Local\Programs\Glint\Glint.exe`)
	if len(removed) != 5 {
		t.Errorf("removed %q, want the two ProgIDs, Applications\\glint.exe and two OpenWithProgids values", removed)
	}
	for _, gone := range []string{`Glint.Image`, `Glint.Video`, `Applications\glint.exe`} {
		if exists(gone) {
			t.Errorf("%s is still registered", gone)
		}
	}
	if got := values(`.png\OpenWithProgids`); !slices.Equal(got, []string{"AppXphotos", "com.glint.app.png"}) {
		t.Errorf(".png opens with %q", got)
	}
	if got := values(`.mp4\OpenWithProgids`); len(got) != 0 {
		t.Errorf(".mp4 opens with %q", got)
	}
	if again := removeRustOpenWith(root, `C:\x\Glint.exe`); len(again) != 0 {
		t.Errorf("a second pass removed %q", again)
	}
}

func TestRemoveRustOpenWithKeepsThisExecutable(t *testing.T) {
	root := fmt.Sprintf(`Software\GlintTest-%d-self`, os.Getpid())
	t.Cleanup(func() { deleteTree(root) })
	exe := `C:\Users\me\AppData\Local\Programs\Glint\Glint.exe`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\Applications\glint.exe\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	k.SetStringValue("", `"C:\USERS\ME\AppData\Local\Programs\Glint\GLINT.EXE" "%1"`)
	k.Close()
	if removed := removeRustOpenWith(root, exe); len(removed) != 0 {
		t.Errorf("removed %q, the command of this Glint.exe", removed)
	}
}
