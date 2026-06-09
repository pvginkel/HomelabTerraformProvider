package cephfssubvolume

import (
	"errors"
	"strings"

	"github.com/ceph/go-ceph/rados"
)

// IsNotFound reports whether err means the subvolume is absent.
//
// cephfs/admin has no exported typed sentinel for this. The mgr command
// surfaces its return code through go-ceph's errutil as a rados errno, so an
// ENOENT (-2) compares equal to rados.ErrNotFound via errors.Is (the errno is
// matched independently of the "rados" source string — see
// internal/errutil.cephError.Is). The substring check is a defensive fallback
// for the case where the mgr embeds the condition only in the status text.
//
// §5.3 of the implementation spec asks to confirm this empirically on the live
// cluster; the errno path is derived from the go-ceph v0.39.0 source and the
// substring covers the remaining case.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rados.ErrNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "does not exist")
}
