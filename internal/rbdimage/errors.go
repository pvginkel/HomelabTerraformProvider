package rbdimage

import (
	"errors"

	"github.com/ceph/go-ceph/rbd"
)

// IsNotFound reports whether err means the RBD image is absent. go-ceph
// returns a typed sentinel for this case.
func IsNotFound(err error) bool {
	return errors.Is(err, rbd.ErrNotFound)
}
