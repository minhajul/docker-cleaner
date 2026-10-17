package docker

import (
	"context"
	"math"

	mobyclient "github.com/moby/moby/client"
)

// BuildCacheID is the ID of the build cache's row and CleanupItem. The angle
// brackets and the space keep it from clashing with a volume name, image,
// container or network ID.
const BuildCacheID = "<build cache>"

// pruneBuildCache removes the unused build cache and returns the space freed.
// all also removes the internal and frontend records a plain prune keeps (see
// BuildCacheUsage.NeedsAll); records in use are never removed.
func pruneBuildCache(cli API, all bool) (int64, error) {
	res, err := cli.BuildCachePrune(context.Background(), mobyclient.BuildCachePruneOptions{All: all})
	if err != nil {
		return 0, err
	}
	reclaimed := res.Report.SpaceReclaimed
	if reclaimed > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(reclaimed), nil
}
