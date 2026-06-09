package cephfssubvolume

import (
	"fmt"

	"github.com/ceph/go-ceph/cephfs/admin"

	"github.com/pvginkel/HomelabTerraformProvider/internal/cephconn"
)

// volume is the cluster's single CephFS filesystem name — a hardcoded
// constant. The subvolume group is the provider's ceph_pool.
const volume = "cephfs"

// Client manages CephFS subvolumes in a fixed filesystem and subvolume group.
// The group is assumed to pre-exist; a missing group surfaces as a clear error
// from the mgr.
type Client struct {
	fsa   *admin.FSAdmin
	group string
}

func NewClient(conn *cephconn.Conn) *Client {
	return &Client{fsa: conn.FSAdmin(), group: conn.Pool()}
}

func (c *Client) Create(name string, sizeBytes uint64) error {
	err := c.fsa.CreateSubVolume(volume, c.group, name, &admin.SubVolumeOptions{
		Size: admin.ByteCount(sizeBytes),
	})
	if err != nil {
		return fmt.Errorf("create cephfs subvolume %q in group %q: %w", name, c.group, err)
	}
	return nil
}

func (c *Client) Read(name string) (*SubVolume, error) {
	info, err := c.fsa.SubVolumeInfo(volume, c.group, name)
	if err != nil {
		return nil, err
	}
	out := &SubVolume{Path: info.Path}
	if bc, ok := info.BytesQuota.(admin.ByteCount); ok {
		out.Bytes = uint64(bc)
		out.QuotaSet = true
	}
	return out, nil
}

// Resize sets the quota to sizeBytes. noShrink is true: the mgr refuses to set
// a quota below current usage.
func (c *Client) Resize(name string, sizeBytes uint64) error {
	_, err := c.fsa.ResizeSubVolume(volume, c.group, name, admin.ByteCount(sizeBytes), true)
	if err != nil {
		return fmt.Errorf("resize cephfs subvolume %q to %d bytes: %w", name, sizeBytes, err)
	}
	return nil
}

// Delete removes the subvolume. A missing subvolume is treated as success.
func (c *Client) Delete(name string) error {
	if err := c.fsa.RemoveSubVolume(volume, c.group, name); err != nil && !IsNotFound(err) {
		return fmt.Errorf("remove cephfs subvolume %q: %w", name, err)
	}
	return nil
}
