package filter

import (
	"slices"
	"testing"
	"time"
)

func TestSortOrderCycles(t *testing.T) {
	want := []string{"default", "size", "age", "name", "default"}
	o := SortDefault
	for i, name := range want {
		if got := o.String(); got != name {
			t.Errorf("after %d presses: order = %q, want %q", i, got, name)
		}
		o = o.Next()
	}
}

func TestSort(t *testing.T) {
	keys := map[string]Key{
		"small-old": {Size: 10, Created: now.Add(-100 * Day), Name: "beta"},
		"large-new": {Size: 300, Created: now.Add(-1 * Day), Name: "Alpha"},
		"mid-noage": {Size: 200, Name: "gamma"},
		"tie-size":  {Size: 200, Created: now.Add(-10 * Day), Name: "alpha"},
	}
	// The daemon's order.
	items := []string{"small-old", "large-new", "mid-noage", "tie-size"}

	cases := []struct {
		order SortOrder
		want  []string
	}{
		{SortDefault, []string{"small-old", "large-new", "mid-noage", "tie-size"}},
		// Ties keep the daemon's order.
		{SortSize, []string{"large-new", "mid-noage", "tie-size", "small-old"}},
		// Oldest first; unknown age last.
		{SortAge, []string{"small-old", "tie-size", "large-new", "mid-noage"}},
		// Case-insensitive: "Alpha" and "alpha" tie.
		{SortName, []string{"large-new", "tie-size", "small-old", "mid-noage"}},
	}
	for _, c := range cases {
		t.Run(c.order.String(), func(t *testing.T) {
			got := slices.Clone(items)
			Sort(got, c.order, func(id string) Key { return keys[id] })
			if !slices.Equal(got, c.want) {
				t.Errorf("Sort(%v) = %v, want %v", c.order, got, c.want)
			}
		})
	}
}

func TestSortAgeUnknownLast(t *testing.T) {
	items := []Key{{Name: "a"}, {Name: "b", Created: now}, {Name: "c"}, {Name: "d", Created: now.Add(-time.Hour)}}
	Sort(items, SortAge, func(k Key) Key { return k })
	var got []string
	for _, k := range items {
		got = append(got, k.Name)
	}
	if want := []string{"d", "b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("Sort by age = %v, want %v", got, want)
	}
}
