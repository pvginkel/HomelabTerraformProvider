package s3storage

import (
	"errors"

	"github.com/aws/smithy-go"
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

// isNoSuchBucketPolicy reports whether err is S3's answer for a bucket that has
// no policy; the SDK has no typed error for it.
func isNoSuchBucketPolicy(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucketPolicy"
}
