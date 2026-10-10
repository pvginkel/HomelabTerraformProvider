package s3reader_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func readerConfig(name, rotation string) string {
	rotationLine := ""
	if rotation != "" {
		rotationLine = fmt.Sprintf("  key_rotation = %q\n", rotation)
	}
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_s3_reader" "test" {
  name = %q
%s}
`, name, rotationLine)
}

func TestAccS3Reader_basic(t *testing.T) {
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))

	var firstAccessKey string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckReaderDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: readerConfig(name, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_s3_reader.test", "id", name),
					resource.TestCheckResourceAttrSet("homelab_s3_reader.test", "access_key_id"),
					resource.TestCheckResourceAttrSet("homelab_s3_reader.test", "secret_access_key"),
					testAccCheckReaderUser(t, name),
					captureAttr("homelab_s3_reader.test", "access_key_id", &firstAccessKey),
				),
			},
			{
				ResourceName:      "homelab_s3_reader.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Rotate the key; the user stays, its access key changes.
				Config: readerConfig(name, "v2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_s3_reader.test", "id", name),
					checkAttrChangedFrom("homelab_s3_reader.test", "access_key_id", &firstAccessKey),
					testAccCheckReaderUser(t, name),
				),
			},
		},
	})
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

// testAccCheckReaderUser checks the live user holds buckets=read and no other
// capability, and can create no bucket.
func testAccCheckReaderUser(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		u, err := liveAdmin(t).GetUser(context.Background(), admin.User{ID: name})
		if err != nil {
			return fmt.Errorf("read rgw user %q: %v", name, err)
		}
		if len(u.Caps) != 1 || u.Caps[0].Type != "buckets" || u.Caps[0].Perm != "read" {
			return fmt.Errorf("rgw user %q caps = %+v, want only buckets=read", name, u.Caps)
		}
		if u.MaxBuckets == nil || *u.MaxBuckets != -1 {
			return fmt.Errorf("rgw user %q max_buckets = %v, want -1", name, u.MaxBuckets)
		}
		return nil
	}
}

func testAccCheckReaderDestroyed(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveAdmin(t).GetUser(context.Background(), admin.User{ID: name})
		if errors.Is(err, admin.ErrNoSuchUser) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unexpected error checking destroyed reader %q: %v", name, err)
		}
		return fmt.Errorf("rgw user %q still exists after destroy", name)
	}
}
