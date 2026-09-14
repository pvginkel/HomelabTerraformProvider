package s3reader

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ceph/go-ceph/rgw/admin"
)

const (
	httpTimeout = 30 * time.Second

	// readerCaps lets the user enumerate buckets through the Admin Ops API;
	// S3 ListBuckets returns only the buckets the caller owns.
	readerCaps = "buckets=read"
)

// Client manages reader users over the RGW Admin Ops API.
type Client struct {
	api *admin.API
}

// NewClient builds a Client against the given RGW endpoint and admin keys.
func NewClient(endpoint, adminAccessKey, adminSecretKey string) (*Client, error) {
	api, err := admin.New(endpoint, adminAccessKey, adminSecretKey, &http.Client{Timeout: httpTimeout})
	if err != nil {
		return nil, fmt.Errorf("init rgw admin api: %w", err)
	}
	return &Client{api: api}, nil
}

// Create makes the RGW user with max_buckets=-1 (can never create buckets) and
// the buckets=read admin capability, and captures its minted key.
func (c *Client) Create(ctx context.Context, name string) (*Reader, error) {
	neg := -1
	u, err := c.api.CreateUser(ctx, admin.User{ID: name, DisplayName: name, MaxBuckets: &neg, UserCaps: readerCaps})
	if err != nil {
		return nil, fmt.Errorf("create rgw user %q: %w", name, err)
	}
	if len(u.Keys) == 0 {
		return nil, fmt.Errorf("rgw user %q was created without a key", name)
	}
	return &Reader{Name: name, AccessKeyID: u.Keys[0].AccessKey, SecretAccessKey: u.Keys[0].SecretKey}, nil
}

// Read returns the user's key matching accessKey, or its first key when none
// matches. The bool is false when the user is gone.
func (c *Client) Read(ctx context.Context, name, accessKey string) (*Reader, bool, error) {
	u, err := c.api.GetUser(ctx, admin.User{ID: name})
	if err != nil {
		if IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read rgw user %q: %w", name, err)
	}

	rd := &Reader{Name: name}
	for _, k := range u.Keys {
		if k.AccessKey == accessKey {
			rd.AccessKeyID, rd.SecretAccessKey = k.AccessKey, k.SecretKey
			return rd, true, nil
		}
	}
	if len(u.Keys) > 0 {
		rd.AccessKeyID, rd.SecretAccessKey = u.Keys[0].AccessKey, u.Keys[0].SecretKey
	}
	return rd, true, nil
}

// Delete removes the user. A missing user is treated as success.
func (c *Client) Delete(ctx context.Context, name string) error {
	if err := c.api.RemoveUser(ctx, admin.User{ID: name}); err != nil && !IsNotFound(err) {
		return fmt.Errorf("remove rgw user %q: %w", name, err)
	}
	return nil
}
