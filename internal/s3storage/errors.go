package s3storage

import (
	"errors"

	"github.com/ceph/go-ceph/rgw/admin"
)

// IsNotFound reports whether err means the RGW user is absent. The rgw/admin
// errors implement errors.Is against typed errorReason constants.
func IsNotFound(err error) bool {
	return errors.Is(err, admin.ErrNoSuchUser)
}

func isNoSuchBucket(err error) bool {
	return errors.Is(err, admin.ErrNoSuchBucket)
}
