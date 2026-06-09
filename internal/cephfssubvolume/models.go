package cephfssubvolume

// SubVolume is the durable CephFS asset: its mount path (the chart's rootPath),
// and its quota in bytes. QuotaSet is false when the subvolume has no finite
// quota (admin.Infinite), in which case Bytes is meaningless.
type SubVolume struct {
	Path     string
	Bytes    uint64
	QuotaSet bool
}
