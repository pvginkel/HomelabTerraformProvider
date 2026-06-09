package rbdimage

import (
	"fmt"

	"github.com/ceph/go-ceph/rbd"

	"github.com/pvginkel/HomelabTerraformProvider/internal/cephconn"
)

// Client creates and manages raw RBD images in the provider's pool. Each call
// opens its own IO context and destroys it; no rados state is held between
// calls.
type Client struct {
	conn *cephconn.Conn
}

func NewClient(conn *cephconn.Conn) *Client {
	return &Client{conn: conn}
}

// Create makes a raw image of sizeBytes with ceph's default order and no
// special features.
func (c *Client) Create(name string, sizeBytes uint64) error {
	ioctx, err := c.conn.OpenIOContext()
	if err != nil {
		return err
	}
	defer ioctx.Destroy()

	if _, err := rbd.Create(ioctx, name, sizeBytes, 0); err != nil {
		return fmt.Errorf("create rbd image %q: %w", name, err)
	}
	return nil
}

// Read returns the image's current size. The rbd.ErrNotFound sentinel is
// propagated unwrapped enough for IsNotFound to match.
func (c *Client) Read(name string) (*Image, error) {
	ioctx, err := c.conn.OpenIOContext()
	if err != nil {
		return nil, err
	}
	defer ioctx.Destroy()

	img, err := rbd.OpenImage(ioctx, name, "")
	if err != nil {
		return nil, err
	}
	defer img.Close()

	sz, err := img.GetSize()
	if err != nil {
		return nil, fmt.Errorf("read size of rbd image %q: %w", name, err)
	}
	return &Image{Name: name, Bytes: sz}, nil
}

// Resize grows the image to sizeBytes. Shrinking is rejected here as a second
// guard (the resource also rejects it in Update) because shrinking an RBD
// image truncates data.
func (c *Client) Resize(name string, sizeBytes uint64) error {
	ioctx, err := c.conn.OpenIOContext()
	if err != nil {
		return err
	}
	defer ioctx.Destroy()

	img, err := rbd.OpenImage(ioctx, name, "")
	if err != nil {
		return err
	}
	defer img.Close()

	cur, err := img.GetSize()
	if err != nil {
		return fmt.Errorf("read size of rbd image %q: %w", name, err)
	}
	if sizeBytes < cur {
		return fmt.Errorf("refusing to shrink rbd image %q from %d to %d bytes: shrinking is unsafe", name, cur, sizeBytes)
	}
	if sizeBytes == cur {
		return nil
	}
	if err := img.Resize(sizeBytes); err != nil {
		return fmt.Errorf("resize rbd image %q to %d bytes: %w", name, sizeBytes, err)
	}
	return nil
}

// Delete removes the image. A missing image is treated as success.
func (c *Client) Delete(name string) error {
	ioctx, err := c.conn.OpenIOContext()
	if err != nil {
		return err
	}
	defer ioctx.Destroy()

	if err := rbd.RemoveImage(ioctx, name); err != nil && !IsNotFound(err) {
		return fmt.Errorf("remove rbd image %q: %w", name, err)
	}
	return nil
}
