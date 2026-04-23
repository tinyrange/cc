package vm

import (
	"context"
	"path"

	"j5.nz/cc/client"
)

func addRuntimeShares(ctx context.Context, inst Instance, shares []client.ShareMount) error {
	for _, share := range shares {
		if err := inst.AddShare(ctx, share); err != nil {
			return err
		}
	}
	return nil
}

func rebaseRuntimeShares(rootDir string, shares []client.ShareMount) []client.ShareMount {
	if rootDir == "" || len(shares) == 0 {
		return append([]client.ShareMount(nil), shares...)
	}
	out := make([]client.ShareMount, 0, len(shares))
	for _, share := range shares {
		rebased := share
		rebased.Mount = path.Join(rootDir, share.Mount)
		out = append(out, rebased)
	}
	return out
}
