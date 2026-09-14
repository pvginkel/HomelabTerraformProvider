package s3reader

import (
	"errors"

	"github.com/ceph/go-ceph/rgw/admin"
)

// IsNotFound reports whether err means the RGW user is absent.
func IsNotFound(err error) bool {
	return errors.Is(err, admin.ErrNoSuchUser)
}
