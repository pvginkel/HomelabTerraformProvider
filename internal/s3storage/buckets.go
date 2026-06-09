package s3storage

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// bucketSlice extracts the bucket names from a set, tolerating null/unknown
// (e.g. a freshly imported resource) by returning an empty slice.
func bucketSlice(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

// diffBuckets returns the buckets present in want but not have (added) and
// those present in have but not want (removed).
func diffBuckets(have, want []string) (added, removed []string) {
	haveSet := make(map[string]struct{}, len(have))
	for _, b := range have {
		haveSet[b] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, b := range want {
		wantSet[b] = struct{}{}
	}
	for _, b := range want {
		if _, ok := haveSet[b]; !ok {
			added = append(added, b)
		}
	}
	for _, b := range have {
		if _, ok := wantSet[b]; !ok {
			removed = append(removed, b)
		}
	}
	return added, removed
}
