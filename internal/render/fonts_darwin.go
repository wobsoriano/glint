//go:build darwin

package render

// DefaultFamily is the platform's default text family.
const DefaultFamily = "SF Pro"

func fontsDir() string { return "/System/Library/Fonts" }

// Paths are relative to /System/Library/Fonts. Collection indices and variable instances come
// from the faces CoreText lists for each file.
var catalog = []family{
	{"SF Pro", [5]*face{instance("SFNS.ttf", 300), instance("SFNS.ttf", 400), instance("SFNS.ttf", 600), instance("SFNS.ttf", 700), instance("SFNS.ttf", 900)}},
	{"New York", [5]*face{nil, instance("NewYork.ttf", 400), instance("NewYork.ttf", 600), instance("NewYork.ttf", 700), instance("NewYork.ttf", 900)}},
	{"SF Mono", [5]*face{instance("SFNSMono.ttf", 300), instance("SFNSMono.ttf", 400), instance("SFNSMono.ttf", 600), instance("SFNSMono.ttf", 700), instance("SFNSMono.ttf", 800)}},
	{"Helvetica Neue", [5]*face{inCollection("HelveticaNeue.ttc", 7), inCollection("HelveticaNeue.ttc", 0), inCollection("HelveticaNeue.ttc", 10), inCollection("HelveticaNeue.ttc", 1), nil}},
	{"Avenir Next", [5]*face{inCollection("Avenir Next.ttc", 10), inCollection("Avenir Next.ttc", 7), inCollection("Avenir Next.ttc", 2), inCollection("Avenir Next.ttc", 0), inCollection("Avenir Next.ttc", 8)}},
	{"Arial", [5]*face{nil, single("Supplemental/Arial.ttf"), nil, single("Supplemental/Arial Bold.ttf"), single("Supplemental/Arial Black.ttf")}},
	{"Georgia", [5]*face{nil, single("Supplemental/Georgia.ttf"), nil, single("Supplemental/Georgia Bold.ttf"), nil}},
	{"Times New Roman", [5]*face{nil, single("Supplemental/Times New Roman.ttf"), nil, single("Supplemental/Times New Roman Bold.ttf"), nil}},
	{"Palatino", [5]*face{nil, inCollection("Palatino.ttc", 0), nil, inCollection("Palatino.ttc", 2), nil}},
	{"Optima", [5]*face{nil, inCollection("Optima.ttc", 0), nil, inCollection("Optima.ttc", 1), inCollection("Optima.ttc", 4)}},
	{"Verdana", [5]*face{nil, single("Supplemental/Verdana.ttf"), nil, single("Supplemental/Verdana Bold.ttf"), nil}},
	{"Tahoma", [5]*face{nil, single("Supplemental/Tahoma.ttf"), nil, single("Supplemental/Tahoma Bold.ttf"), nil}},
	{"Trebuchet MS", [5]*face{nil, single("Supplemental/Trebuchet MS.ttf"), nil, single("Supplemental/Trebuchet MS Bold.ttf"), nil}},
	{"Menlo", [5]*face{nil, inCollection("Menlo.ttc", 0), nil, inCollection("Menlo.ttc", 1), nil}},
	{"Courier New", [5]*face{nil, single("Supplemental/Courier New.ttf"), nil, single("Supplemental/Courier New Bold.ttf"), nil}},
	{"Comic Sans MS", [5]*face{nil, single("Supplemental/Comic Sans MS.ttf"), nil, single("Supplemental/Comic Sans MS Bold.ttf"), nil}},
	{"Impact", [5]*face{nil, single("Supplemental/Impact.ttf"), nil, nil, nil}},
}
