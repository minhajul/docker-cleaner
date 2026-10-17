package models

import (
	"testing"

	"github.com/minhajul/docker-cleaner/docker"
)

func TestItemTypesMatchCleanupItemTypes(t *testing.T) {
	cases := []struct {
		item    ItemType
		cleanup docker.CleanupItemType
	}{
		{ItemTypeImage, docker.CleanupItemTypeImage},
		{ItemTypeContainer, docker.CleanupItemTypeContainer},
		{ItemTypeVolume, docker.CleanupItemTypeVolume},
		{ItemTypeNetwork, docker.CleanupItemTypeNetwork},
		{ItemTypeBuildCache, docker.CleanupItemTypeBuildCache},
	}
	for _, c := range cases {
		if int(c.item) != int(c.cleanup) {
			t.Errorf("ItemType %d does not match CleanupItemType %d", c.item, c.cleanup)
		}
	}
}
