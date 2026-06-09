package s3storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/ceph/go-ceph/rgw/admin"
)

const httpTimeout = 30 * time.Second

// bucketCreator abstracts the one operation the RGW Admin Ops API cannot do —
// creating a bucket — so the rgw-only logic stays unit-testable with httptest.
// The real implementation issues a SigV4 PutBucket via aws-sdk-go-v2.
type bucketCreator interface {
	CreateBucket(ctx context.Context, bucket string) error
}

// Client composes the RGW Admin Ops API (user/key/bucket ownership) with a
// bucket creator (the only path that needs real S3).
type Client struct {
	api *admin.API
	s3  bucketCreator
}

// NewClient builds a Client against the given RGW endpoint and admin keys.
func NewClient(endpoint, adminAccessKey, adminSecretKey, version string) (*Client, error) {
	httpClient := &http.Client{Timeout: httpTimeout}

	api, err := admin.New(endpoint, adminAccessKey, adminSecretKey, httpClient)
	if err != nil {
		return nil, fmt.Errorf("init rgw admin api: %w", err)
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(adminAccessKey, adminSecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return &Client{api: api, s3: &awsBucketCreator{client: s3c}}, nil
}

// awsBucketCreator is the production bucketCreator over aws-sdk-go-v2.
type awsBucketCreator struct {
	client *s3.Client
}

func (a *awsBucketCreator) CreateBucket(ctx context.Context, bucket string) error {
	_, err := a.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil
	}
	// Owning or pre-existing the bucket is idempotent success.
	var owned *awss3types.BucketAlreadyOwnedByYou
	var exists *awss3types.BucketAlreadyExists
	if errors.As(err, &owned) || errors.As(err, &exists) {
		return nil
	}
	return fmt.Errorf("create bucket %q: %w", bucket, err)
}

// Create makes the RGW user with max_buckets=-1 (can never create buckets
// itself), captures its minted key, then creates and links each bucket.
func (c *Client) Create(ctx context.Context, name string, buckets []string) (*Storage, error) {
	neg := -1
	u, err := c.api.CreateUser(ctx, admin.User{ID: name, DisplayName: name, MaxBuckets: &neg})
	if err != nil {
		return nil, fmt.Errorf("create rgw user %q: %w", name, err)
	}
	if len(u.Keys) == 0 {
		return nil, fmt.Errorf("rgw user %q was created without a key", name)
	}

	if err := c.AddBuckets(ctx, name, buckets); err != nil {
		return nil, err
	}

	return &Storage{
		Name:            name,
		AccessKeyID:     u.Keys[0].AccessKey,
		SecretAccessKey: u.Keys[0].SecretKey,
		Buckets:         buckets,
	}, nil
}

// Read returns the user's current key and the subset of wantBuckets that still
// exist and are owned by the user. The bool is false when the user is gone.
func (c *Client) Read(ctx context.Context, name, accessKey string, wantBuckets []string) (*Storage, bool, error) {
	u, err := c.api.GetUser(ctx, admin.User{ID: name})
	if err != nil {
		if IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read rgw user %q: %w", name, err)
	}

	access, secret := selectKey(u.Keys, accessKey)

	owned := make([]string, 0, len(wantBuckets))
	for _, b := range wantBuckets {
		info, err := c.api.GetBucketInfo(ctx, admin.Bucket{Bucket: b})
		if err != nil {
			if isNoSuchBucket(err) {
				continue
			}
			return nil, false, fmt.Errorf("read bucket %q: %w", b, err)
		}
		if info.Owner == name {
			owned = append(owned, b)
		}
	}

	return &Storage{Name: name, AccessKeyID: access, SecretAccessKey: secret, Buckets: owned}, true, nil
}

// AddBuckets creates each bucket with the admin credential, then links it to
// the per-release user (which also unlinks it from the admin).
func (c *Client) AddBuckets(ctx context.Context, name string, buckets []string) error {
	for _, b := range buckets {
		if err := c.s3.CreateBucket(ctx, b); err != nil {
			return err
		}
		if err := c.api.LinkBucket(ctx, admin.BucketLinkInput{Bucket: b, UID: name}); err != nil {
			return fmt.Errorf("link bucket %q to user %q: %w", b, name, err)
		}
	}
	return nil
}

// RemoveBuckets deletes each bucket and all its objects. A missing bucket is
// treated as success.
func (c *Client) RemoveBuckets(ctx context.Context, buckets []string) error {
	purge := true
	for _, b := range buckets {
		if err := c.api.RemoveBucket(ctx, admin.Bucket{Bucket: b, PurgeObject: &purge}); err != nil && !isNoSuchBucket(err) {
			return fmt.Errorf("remove bucket %q: %w", b, err)
		}
	}
	return nil
}

// RotateKey mints a fresh s3 key on the user, then removes the old one. The
// buckets are untouched.
func (c *Client) RotateKey(ctx context.Context, name, oldAccessKey string) (access, secret string, err error) {
	keys, err := c.api.CreateKey(ctx, admin.UserKeySpec{UID: name, KeyType: "s3"})
	if err != nil {
		return "", "", fmt.Errorf("create new key for user %q: %w", name, err)
	}
	access, secret = newestKey(keys, oldAccessKey)
	if access == "" {
		return "", "", fmt.Errorf("rgw did not return a new key for user %q", name)
	}
	if err := c.api.RemoveKey(ctx, admin.UserKeySpec{UID: name, AccessKey: oldAccessKey, KeyType: "s3"}); err != nil {
		return "", "", fmt.Errorf("remove old key for user %q: %w", name, err)
	}
	return access, secret, nil
}

// Delete purges every bucket (objects included) and removes the user.
func (c *Client) Delete(ctx context.Context, name string, buckets []string) error {
	if err := c.RemoveBuckets(ctx, buckets); err != nil {
		return err
	}
	if err := c.api.RemoveUser(ctx, admin.User{ID: name}); err != nil && !IsNotFound(err) {
		return fmt.Errorf("remove rgw user %q: %w", name, err)
	}
	return nil
}

// selectKey returns the access/secret pair matching accessKey, falling back to
// the first key. RGW returns secrets on GetUser, so this reconstructs both.
func selectKey(keys []admin.UserKeySpec, accessKey string) (string, string) {
	for _, k := range keys {
		if k.AccessKey == accessKey {
			return k.AccessKey, k.SecretKey
		}
	}
	if len(keys) > 0 {
		return keys[0].AccessKey, keys[0].SecretKey
	}
	return "", ""
}

// newestKey returns the key whose access key differs from oldAccessKey.
func newestKey(keys *[]admin.UserKeySpec, oldAccessKey string) (string, string) {
	if keys == nil {
		return "", ""
	}
	for _, k := range *keys {
		if k.AccessKey != oldAccessKey {
			return k.AccessKey, k.SecretKey
		}
	}
	return "", ""
}
