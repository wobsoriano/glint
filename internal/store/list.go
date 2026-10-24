package store

import (
	"slices"
	"strings"

	"github.com/wobsoriano/glint/internal/settings"
)

// Filter is the Library sidebar's choice of which kinds to show.
type Filter int

const (
	FilterAll Filter = iota
	FilterScreenshots
	FilterRecordings
)

func (f Filter) Admits(k Kind) bool {
	switch f {
	case FilterScreenshots:
		return k == Screenshot
	case FilterRecordings:
		return k == Recording
	}
	return true
}

// Visible is what the Library window lists: items the filter admits whose title contains
// search, ignoring case, in the chosen order. Equal keys keep their order in items.
func Visible(items []Item, f Filter, search string, sort settings.SortKey) []Item {
	q := strings.ToLower(strings.TrimSpace(search))
	var out []Item
	for _, it := range items {
		if f.Admits(it.Kind()) && (q == "" || strings.Contains(strings.ToLower(it.Meta.Title), q)) {
			out = append(out, it)
		}
	}
	switch sort {
	case settings.SortCaptured:
		slices.SortStableFunc(out, func(a, b Item) int { return b.Meta.Created.Compare(a.Meta.Created) })
	case settings.SortModified:
		slices.SortStableFunc(out, func(a, b Item) int { return b.Meta.Modified.Compare(a.Meta.Modified) })
	case settings.SortName:
		slices.SortStableFunc(out, func(a, b Item) int {
			return strings.Compare(strings.ToLower(a.Meta.Title), strings.ToLower(b.Meta.Title))
		})
	}
	return out
}

// Upsert replaces the item with the same directory, or inserts it so a newest-first list
// stays newest first.
func Upsert(items []Item, item Item) []Item {
	if i := slices.IndexFunc(items, func(x Item) bool { return x.Dir == item.Dir }); i >= 0 {
		items[i] = item
		return items
	}
	at := slices.IndexFunc(items, func(x Item) bool { return !x.Meta.Created.After(item.Meta.Created) })
	if at < 0 {
		at = len(items)
	}
	return slices.Insert(items, at, item)
}

// DropTrashed removes the items Trash was asked to move whose directories are gone. One the
// trash refused stays listed.
func DropTrashed(items []Item, dirs []string) []Item {
	return slices.DeleteFunc(items, func(x Item) bool { return slices.Contains(dirs, x.Dir) && !exists(x.Dir) })
}
