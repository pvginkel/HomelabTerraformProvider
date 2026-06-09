// Package cephconn holds the single librados connection the provider shares
// between the rbd and cephfs clients. It is cgo (librados); building it needs
// the Ceph dev headers and CGO_ENABLED=1.
package cephconn

import (
	"fmt"

	"github.com/ceph/go-ceph/cephfs/admin"
	"github.com/ceph/go-ceph/rados"
)

// Conn wraps a connected *rados.Conn plus the pool name that serves double
// duty as the RBD pool and the CephFS subvolume group (always equal per env).
type Conn struct {
	conn *rados.Conn
	pool string
}

// New opens and connects a librados handle. user is the cephx user WITHOUT the
// "client." prefix; key is the inline base64 cephx key; monHost may be a
// comma-separated list of mon addresses.
func New(monHost, user, key, pool string) (*Conn, error) {
	conn, err := rados.NewConnWithUser(user)
	if err != nil {
		return nil, fmt.Errorf("create rados connection for user %q: %w", user, err)
	}
	if err := conn.SetConfigOption("mon_host", monHost); err != nil {
		return nil, fmt.Errorf("set mon_host: %w", err)
	}
	if err := conn.SetConfigOption("key", key); err != nil {
		return nil, fmt.Errorf("set cephx key: %w", err)
	}
	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect to ceph cluster at %q: %w", monHost, err)
	}
	return &Conn{conn: conn, pool: pool}, nil
}

// Pool returns the configured pool / subvolume-group name.
func (c *Conn) Pool() string { return c.pool }

// OpenIOContext opens a fresh IO context on the configured pool. Callers must
// Destroy() it when done; don't hold one across resource calls.
func (c *Conn) OpenIOContext() (*rados.IOContext, error) {
	ioctx, err := c.conn.OpenIOContext(c.pool)
	if err != nil {
		return nil, fmt.Errorf("open io context on pool %q: %w", c.pool, err)
	}
	return ioctx, nil
}

// FSAdmin returns a CephFS admin handle bound to this connection.
func (c *Conn) FSAdmin() *admin.FSAdmin {
	return admin.NewFromConn(c.conn)
}

// Close shuts the connection down. Held for the provider-process lifetime; the
// framework has no provider-close hook, so process exit normally reclaims it.
func (c *Conn) Close() {
	if c.conn != nil {
		c.conn.Shutdown()
	}
}
