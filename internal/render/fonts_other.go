//go:build !darwin

package render

import (
	"os"
	"path/filepath"
)

// DefaultFamily is the platform's default text family.
const DefaultFamily = "Segoe UI"

func fontsDir() string {
	dir := os.Getenv("WINDIR")
	if dir == "" {
		dir = `C:\Windows`
	}
	return filepath.Join(dir, "Fonts")
}

var catalog = []family{
	{"Segoe UI", [5]*face{single("segoeuil.ttf"), single("segoeui.ttf"), single("seguisb.ttf"), single("segoeuib.ttf"), single("seguibl.ttf")}},
	{"Arial", [5]*face{nil, single("arial.ttf"), nil, single("arialbd.ttf"), single("ariblk.ttf")}},
	{"Calibri", [5]*face{single("calibril.ttf"), single("calibri.ttf"), nil, single("calibrib.ttf"), nil}},
	{"Cambria", [5]*face{nil, inCollection("cambria.ttc", 0), nil, single("cambriab.ttf"), nil}},
	{"Candara", [5]*face{single("Candaral.ttf"), single("Candara.ttf"), nil, single("Candarab.ttf"), nil}},
	{"Corbel", [5]*face{single("corbell.ttf"), single("corbel.ttf"), nil, single("corbelb.ttf"), nil}},
	{"Constantia", [5]*face{nil, single("constan.ttf"), nil, single("constanb.ttf"), nil}},
	{"Georgia", [5]*face{nil, single("georgia.ttf"), nil, single("georgiab.ttf"), nil}},
	{"Times New Roman", [5]*face{nil, single("times.ttf"), nil, single("timesbd.ttf"), nil}},
	{"Palatino Linotype", [5]*face{nil, single("pala.ttf"), nil, single("palab.ttf"), nil}},
	{"Verdana", [5]*face{nil, single("verdana.ttf"), nil, single("verdanab.ttf"), nil}},
	{"Tahoma", [5]*face{nil, single("tahoma.ttf"), nil, single("tahomabd.ttf"), nil}},
	{"Trebuchet MS", [5]*face{nil, single("trebuc.ttf"), nil, single("trebucbd.ttf"), nil}},
	{"Bahnschrift", [5]*face{nil, single("bahnschrift.ttf"), nil, nil, nil}},
	{"Consolas", [5]*face{nil, single("consola.ttf"), nil, single("consolab.ttf"), nil}},
	{"Cascadia Code", [5]*face{nil, single("CascadiaCode.ttf"), nil, nil, nil}},
	{"Comic Sans MS", [5]*face{nil, single("comic.ttf"), nil, single("comicbd.ttf"), nil}},
	{"Impact", [5]*face{nil, single("impact.ttf"), nil, nil, nil}},
	{"Segoe Print", [5]*face{nil, single("segoepr.ttf"), nil, single("segoeprb.ttf"), nil}},
	{"Segoe Script", [5]*face{nil, single("segoesc.ttf"), nil, single("segoescb.ttf"), nil}},
}
