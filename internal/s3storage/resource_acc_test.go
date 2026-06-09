package s3storage_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
