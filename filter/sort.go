package filter

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// SortOrder is an order to list resources in.
type SortOrder int

const (
	// SortDefault keeps the order the daemon listed them in.
	SortDefault SortOrder = iota
	// SortSize puts the largest first.
	SortSize
	// SortAge puts the oldest first, the likeliest to be stale; those
	// whose creation time is unknown go last.
	SortAge
	// SortName sorts by name, ignoring case.
	SortName
)

// Next returns the order after o, cycling default → size → age → name →
// default.
func (o SortOrder) Next() SortOrder {
	return (o + 1) % (SortName + 1)
}

// String names the order, e.g. "size".
func (o SortOrder) String() string {
	switch o {
	case SortSize:
		return "size"
	case SortAge:
		return "age"
	case SortName:
		return "name"
	}
	return "default"
}

// Key is what a resource is sorted by.
type Key struct {
	// Size is the space deleting the resource frees, in bytes.
	Size int64
	// Created is when the resource was created; zero if unknown.
	Created time.Time
	// Name is the name the resource is shown by.
	Name string
}

// Sort sorts items in place in order o, by the Key key returns for each.
// The sort is stable, so items that tie, and all items under SortDefault,
// keep their order.
func Sort[T any](items []T, o SortOrder, key func(T) Key) {
	if o == SortDefault {
		return
	}
	slices.SortStableFunc(items, func(a, b T) int {
		return compare(key(a), key(b), o)
	})
}

// compare orders a and b by o, as slices.SortFunc expects.
func compare(a, b Key, o SortOrder) int {
	switch o {
	case SortSize:
		return cmp.Compare(b.Size, a.Size)
	case SortAge:
		// Unknown creation times go last.
		if c := compareBool(a.Created.IsZero(), b.Created.IsZero()); c != 0 {
			return c
		}
		return a.Created.Compare(b.Created)
	case SortName:
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
	return 0
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}
