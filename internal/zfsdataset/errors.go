package zfsdataset

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError wraps a non-2xx response from an iac-provisioner agent. Code/Message
// come from the JSON envelope when present; otherwise Message falls back to the
// HTTP status text.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// UnmappedPoolError is returned when a dataset names a pool that has no entry in
// the provider's zfs_pools map, so the agent host cannot be resolved. It is a
// configuration error surfaced at plan/apply time, before any HTTP call.
type UnmappedPoolError struct {
	Pool string
}

func (e *UnmappedPoolError) Error() string {
	return fmt.Sprintf("pool %q has no zfs_pools mapping; add it to the provider's zfs_pools (pool -> node hostname)", e.Pool)
}
