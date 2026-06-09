package cephfssubvolume_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pvginkel/HomelabTerraformProvider/internal/cephfssubvolume"
)

func subvolumeConfig(name, size string) string {
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_cephfs_subvolume" "test" {
  name = %q
  size = %q
}
`, name, size)
}

func TestAccCephFSSubVolume_basic(t *testing.T) {
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckSubVolumeDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: subvolumeConfig(name, "1Gi"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_cephfs_subvolume.test", "id", name),
					resource.TestCheckResourceAttr("homelab_cephfs_subvolume.test", "name", name),
					resource.TestCheckResourceAttr("homelab_cephfs_subvolume.test", "size", "1Gi"),
					resource.TestCheckResourceAttrSet("homelab_cephfs_subvolume.test", "path"),
				),
			},
			{
				ResourceName:      "homelab_cephfs_subvolume.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			{
				// Grow in place.
				Config: subvolumeConfig(name, "2Gi"),
				Check:  resource.TestCheckResourceAttr("homelab_cephfs_subvolume.test", "size", "2Gi"),
			},
		},
	})
}

func testAccCheckSubVolumeDestroyed(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveClient(t).Read(name)
		if err == nil {
			return fmt.Errorf("cephfs subvolume %q still exists after destroy", name)
		}
		if !cephfssubvolume.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed subvolume %q: %v", name, err)
		}
		return nil
	}
}
