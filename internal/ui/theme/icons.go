package theme

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

// IconName is one of Glint's icons: outlined, on a 24-unit grid with a 2-unit round stroke,
// drawn in the color of the text by ui.Icon.
type IconName int

// IconHead opens every icon's SVG; the body and "</svg>" follow.
const IconHead = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`

var (
	iconOnce sync.Once
	iconSVGs [iconCount]*ui.SVG
)

// Icon returns the parsed icon, for ui.Icon and Painter.Icon.
func Icon(n IconName) *ui.SVG {
	iconOnce.Do(func() {
		for i := range iconSVGs {
			iconSVGs[i] = ui.MustParseSVG([]byte(IconSource(IconName(i))))
		}
	})
	return iconSVGs[n]
}

// IconSource returns the icon's SVG document.
func IconSource(n IconName) string { return IconHead + iconBodies[n] + "</svg>" }

func (n IconName) String() string { return iconNames[n] }

// IconByName returns the icon with the name the Rust build used, as "save-as".
func IconByName(name string) (IconName, bool) {
	for i, s := range iconNames {
		if s == name {
			return IconName(i), true
		}
	}
	return 0, false
}

// Icons returns every icon.
func Icons() []IconName {
	out := make([]IconName, iconCount)
	for i := range out {
		out[i] = IconName(i)
	}
	return out
}
