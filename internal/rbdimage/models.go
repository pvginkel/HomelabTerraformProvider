package rbdimage

// Image is the durable RBD asset as the provider sees it: its name and current
// size in bytes. Created raw (no map/mkfs) — the ceph-csi rbd driver formats
// it on first mount.
type Image struct {
	Name  string
	Bytes uint64
}
