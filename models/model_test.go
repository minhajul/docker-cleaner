package models

import "testing"

func TestGetSelectionForCleanup(t *testing.T) {
	model := InitialModel()
	model.items = []SelectableItem{
		{ID: "img1", Type: ItemTypeImage},
		{ID: "ctr1", Type: ItemTypeContainer},
	}
	model.selected = map[int]struct{}{0: {}, 1: {}}

	items := model.getSelectionForCleanup()
	if len(items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(items))
	}
}
