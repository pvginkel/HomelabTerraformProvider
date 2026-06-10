package zfsdataset_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pvginkel/HomelabTerraformProvider/internal/zfsdataset"
)

// datasetConfig builds a single-resource HCL config. The provider's zfs_pools
// map points the scratch pool at the test host; the token comes from
// HOMELAB_ZFS_PROVISIONER_TOKEN via the env fallback.
func datasetConfig(pool, host, name, compression string) string {
	return fmt.Sprintf(`
provider "homelab" {
  zfs_pools = { %q = %q }
}

resource "homelab_zfs_dataset" "test" {
  pool        = %q
  name        = %q
  quota       = "1G"
  compression = %q
}
`, pool, host, pool, name, compression)
}

func TestAccZFSDataset_basic(t *testing.T) {
	pool := testPool()
	host := testHost()
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))
	id := pool + "/" + name

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatasetDestroyed(pool, name),
		Steps: []resource.TestStep{
			{
				Config: datasetConfig(pool, host, name, "lz4"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "id", id),
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "pool", pool),
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "name", name),
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "quota", "1G"),
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "recordsize", "128K"),
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "compression", "lz4"),
					resource.TestCheckResourceAttrSet("homelab_zfs_dataset.test", "guid"),
					resource.TestCheckResourceAttrSet("homelab_zfs_dataset.test", "mountpoint_resolved"),
				),
			},
			{
				ResourceName:      "homelab_zfs_dataset.test",
				ImportState:       true,
				ImportStateId:     id,
				ImportStateVerify: true,
			},
			{
				// Non-destructive property update: compression lz4 -> zstd.
				Config: datasetConfig(pool, host, name, "zstd"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_zfs_dataset.test", "compression", "zstd"),
					checkGUIDStable("homelab_zfs_dataset.test"),
				),
			},
		},
	})
}

// checkGUIDStable asserts the dataset keeps its GUID across a property update —
// the update was applied in place, not as a destroy + recreate.
func checkGUIDStable(resourceName string) resource.TestCheckFunc {
	var first string
	captured := false
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resourceName)
		}
		guid := rs.Primary.Attributes["guid"]
		if guid == "" {
			return fmt.Errorf("guid is empty")
		}
		if !captured {
			first, captured = guid, true
		} else if guid != first {
			return fmt.Errorf("guid changed across update: %q -> %q", first, guid)
		}
		return nil
	}
}

func testAccCheckDatasetDestroyed(pool, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveClient().Get(context.Background(), pool, name)
		if err == nil {
			return fmt.Errorf("dataset %s/%s still exists after destroy", pool, name)
		}
		if !zfsdataset.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed dataset %s/%s: %v", pool, name, err)
		}
		return nil
	}
}
