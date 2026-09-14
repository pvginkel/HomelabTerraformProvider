package s3storage

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

type policyStatement struct {
	Effect    string              `json:"Effect"`
	Principal map[string][]string `json:"Principal"`
	Action    []string            `json:"Action"`
	Resource  []string            `json:"Resource"`
}

// readerPolicy is the whole policy a granted bucket carries: the reader may
// list the bucket and get its objects, and nothing that writes or deletes.
func readerPolicy(reader, bucket string) string {
	principal := map[string][]string{"AWS": {"arn:aws:iam:::user/" + reader}}
	doc := policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{Effect: "Allow", Principal: principal, Action: []string{"s3:ListBucket"}, Resource: []string{"arn:aws:s3:::" + bucket}},
			{Effect: "Allow", Principal: principal, Action: []string{"s3:GetObject"}, Resource: []string{"arn:aws:s3:::" + bucket + "/*"}},
		},
	}
	// Marshalling a document of strings, slices and string maps cannot fail.
	out, _ := json.Marshal(doc)
	return string(out)
}

// policyEqual compares two policy documents as JSON values, ignoring layout
// and key order.
func policyEqual(a, b string) bool {
	var av, bv any
	if json.Unmarshal([]byte(a), &av) != nil || json.Unmarshal([]byte(b), &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// HasReader reports whether the provider names a reader. Without one the grant
// is never written, removed or checked.
func (c *Client) HasReader() bool {
	return c.reader != ""
}

// GrantReader writes the reader policy on each bucket, replacing any policy the
// bucket had. RGW takes a bucket policy only from the bucket's owner, so the
// owning user's key signs the request.
func (c *Client) GrantReader(ctx context.Context, accessKey, secretKey string, buckets []string) error {
	s3c, err := newS3Client(ctx, c.endpoint, accessKey, secretKey)
	if err != nil {
		return err
	}
	for _, b := range buckets {
		if _, err := s3c.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
			Bucket: aws.String(b),
			Policy: aws.String(readerPolicy(c.reader, b)),
		}); err != nil {
			return fmt.Errorf("put reader policy on bucket %q: %w", b, err)
		}
	}
	return nil
}

// RevokeReader deletes each bucket's policy. A bucket without one is already
// revoked.
func (c *Client) RevokeReader(ctx context.Context, accessKey, secretKey string, buckets []string) error {
	s3c, err := newS3Client(ctx, c.endpoint, accessKey, secretKey)
	if err != nil {
		return err
	}
	for _, b := range buckets {
		if _, err := s3c.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(b)}); err != nil && !isNoSuchBucketPolicy(err) {
			return fmt.Errorf("delete policy of bucket %q: %w", b, err)
		}
	}
	return nil
}

// ReaderGranted reports whether every bucket carries exactly the reader policy.
func (c *Client) ReaderGranted(ctx context.Context, accessKey, secretKey string, buckets []string) (bool, error) {
	s3c, err := newS3Client(ctx, c.endpoint, accessKey, secretKey)
	if err != nil {
		return false, err
	}
	for _, b := range buckets {
		out, err := s3c.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(b)})
		if err != nil {
			if isNoSuchBucketPolicy(err) {
				return false, nil
			}
			return false, fmt.Errorf("read policy of bucket %q: %w", b, err)
		}
		if !policyEqual(aws.ToString(out.Policy), readerPolicy(c.reader, b)) {
			return false, nil
		}
	}
	return true, nil
}
