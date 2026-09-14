package s3storage_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func storageConfig(name, rotation string, buckets ...string) string {
	quoted := make([]string, len(buckets))
	for i, b := range buckets {
		quoted[i] = fmt.Sprintf("%q", b)
	}
	rotationLine := ""
	if rotation != "" {
		rotationLine = fmt.Sprintf("  key_rotation = %q\n", rotation)
	}
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_s3_storage" "test" {
  name    = %q
  buckets = [%s]
%s}
`, name, strings.Join(quoted, ", "), rotationLine)
}

func TestAccS3Storage_basic(t *testing.T) {
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))
	b1 := name + "-one"
	b2 := name + "-two"

	var firstAccessKey string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStorageDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: storageConfig(name, "", b1, b2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_s3_storage.test", "id", name),
					resource.TestCheckResourceAttr("homelab_s3_storage.test", "buckets.#", "2"),
					resource.TestCheckResourceAttrSet("homelab_s3_storage.test", "access_key_id"),
					resource.TestCheckResourceAttrSet("homelab_s3_storage.test", "secret_access_key"),
					captureAttr("homelab_s3_storage.test", "access_key_id", &firstAccessKey),
				),
			},
			{
				// Remove one bucket, add another.
				Config: storageConfig(name, "", b1, name+"-three"),
				Check:  resource.TestCheckResourceAttr("homelab_s3_storage.test", "buckets.#", "2"),
			},
			{
				// Rotate the key; buckets stay, access key changes.
				Config: storageConfig(name, "v2", b1, name+"-three"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_s3_storage.test", "buckets.#", "2"),
					checkAttrChangedFrom("homelab_s3_storage.test", "access_key_id", &firstAccessKey),
				),
			},
		},
	})
}

func grantConfig(name, reader string, grant bool) string {
	grantLine := ""
	if grant {
		grantLine = "  grant_backup_reader = true\n"
	}
	return fmt.Sprintf(`
provider "homelab" {
  s3_backup_reader = %q
}

resource "homelab_s3_reader" "reader" {
  name = %q
}

resource "homelab_s3_storage" "test" {
  name       = %q
  buckets    = [%q]
  depends_on = [homelab_s3_reader.reader]
%s}
`, reader, reader, name, name+"-one", grantLine)
}

func TestAccS3Storage_readerGrant(t *testing.T) {
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))
	reader := name + "-reader"
	bucket := name + "-one"

	var owner, readerKey keyPair

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStorageDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: grantConfig(name, reader, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureKeys("homelab_s3_storage.test", &owner),
					captureKeys("homelab_s3_reader.reader", &readerKey),
					testAccCheckReaderReadOnly(bucket, &owner, &readerKey),
				),
			},
			{
				// A policy removed out of band is drift the apply restores in place.
				PreConfig: func() {
					if _, err := liveS3(owner).DeleteBucketPolicy(context.Background(), &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)}); err != nil {
						t.Fatalf("delete policy of %s out of band: %v", bucket, err)
					}
				},
				Config: grantConfig(name, reader, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homelab_s3_storage.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: testAccCheckReaderReadOnly(bucket, &owner, &readerKey),
			},
			{
				Config: grantConfig(name, reader, false),
				Check:  testAccCheckNoPolicy(bucket, &owner),
			},
		},
	})
}

func captureKeys(resourceName string, dst *keyPair) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resourceName)
		}
		dst.access = rs.Primary.Attributes["access_key_id"]
		dst.secret = rs.Primary.Attributes["secret_access_key"]
		return nil
	}
}

func hasErrorCode(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == code
}

// testAccCheckReaderReadOnly checks the reader lists the bucket and gets an
// object the owner wrote, and is refused a write and a delete.
func testAccCheckReaderReadOnly(bucket string, owner, reader *keyPair) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		ctx := context.Background()
		if _, err := liveS3(*owner).PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket), Key: aws.String("probe"), Body: strings.NewReader("probe"),
		}); err != nil {
			return fmt.Errorf("owner put into %s: %v", bucket, err)
		}

		rc := liveS3(*reader)
		if _, err := rc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}); err != nil {
			return fmt.Errorf("reader list %s: %v", bucket, err)
		}
		obj, err := rc.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("probe")})
		if err != nil {
			return fmt.Errorf("reader get from %s: %v", bucket, err)
		}
		_ = obj.Body.Close()
		if _, err := rc.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket), Key: aws.String("reader-write"), Body: strings.NewReader("x"),
		}); !hasErrorCode(err, "AccessDenied") {
			return fmt.Errorf("reader put into %s: err = %v, want AccessDenied", bucket, err)
		}
		if _, err := rc.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("probe")}); !hasErrorCode(err, "AccessDenied") {
			return fmt.Errorf("reader delete in %s: err = %v, want AccessDenied", bucket, err)
		}
		return nil
	}
}

func testAccCheckNoPolicy(bucket string, owner *keyPair) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveS3(*owner).GetBucketPolicy(context.Background(), &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
		if !hasErrorCode(err, "NoSuchBucketPolicy") {
			return fmt.Errorf("policy of %s after dropping the ask: err = %v, want NoSuchBucketPolicy", bucket, err)
		}
		return nil
	}
}

func captureAttr(resourceName, attr string, dst *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resourceName)
		}
		*dst = rs.Primary.Attributes[attr]
		return nil
	}
}

func checkAttrChangedFrom(resourceName, attr string, prev *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resourceName)
		}
		if got := rs.Primary.Attributes[attr]; got == *prev {
			return fmt.Errorf("%s.%s = %q did not change after rotation", resourceName, attr, got)
		}
		return nil
	}
}

func testAccCheckStorageDestroyed(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, found, err := liveClient(t).Read(context.Background(), name, "", nil)
		if err != nil {
			return fmt.Errorf("unexpected error checking destroyed storage %q: %v", name, err)
		}
		if found {
			return fmt.Errorf("rgw user %q still exists after destroy", name)
		}
		return nil
	}
}
