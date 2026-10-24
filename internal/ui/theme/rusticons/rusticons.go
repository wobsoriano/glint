// Package rusticons reads the icon table of the Rust build's src/assets.rs, for the generator and
// the parity test of package theme. It goes when the Rust tree does.
package rusticons

import "regexp"

type Icon struct{ Name, Body string }

var entry = regexp.MustCompile(`(?m)^\s+"([a-z-]+)" => r##"(.*)"##,$`)

// Parse returns the icons of assets.rs in their order there.
func Parse(src string) []Icon {
	var out []Icon
	for _, m := range entry.FindAllStringSubmatch(src, -1) {
		out = append(out, Icon{Name: m[1], Body: m[2]})
	}
	return out
}
