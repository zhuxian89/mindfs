package assetsync

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"mindfs-cloud/internal/config"
)

type Coverage struct {
	Releases  int
	LatestTag string
}

// Check reads the release catalog and local files without downloading archives
// or writing assets. Run it during release maintenance, not on the request path.
func Check(ctx context.Context, options Options) (Coverage, error) {
	var coverage Coverage
	if strings.TrimSpace(options.TargetDir) == "" {
		return coverage, fmt.Errorf("target asset directory is required")
	}
	if err := config.CheckAssetsDir(options.TargetDir); err != nil {
		return coverage, err
	}
	releases, _, err := supportedReleases(ctx, options)
	if err != nil {
		return coverage, err
	}
	var incomplete []string
	for _, item := range releases {
		if err := ctx.Err(); err != nil {
			return coverage, err
		}
		asset, ok := linuxReleaseAsset(item)
		marker := filepath.Join(options.TargetDir, ".releases", item.TagName+".complete")
		if !ok || !markerMatches(marker, options.TargetDir, asset) {
			incomplete = append(incomplete, item.TagName)
			continue
		}
		coverage.Releases++
		coverage.LatestTag = item.TagName
	}
	if len(incomplete) > 0 {
		return coverage, fmt.Errorf("release assets missing, changed or unverified for %s; run sync-assets and resolve any reported collisions", strings.Join(incomplete, ", "))
	}
	return coverage, nil
}
