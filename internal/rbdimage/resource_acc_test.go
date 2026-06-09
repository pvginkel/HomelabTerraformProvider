package rbdimage_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pvginkel/HomelabTerraformProvider/internal/rbdimage"
)

func imageConfig(name, size string) string {
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_rbd_image" "test" {
  name = %q
  size = %q
}
`, name, size)
}

func TestAccRBDImage_basic(t *testing.T) {
	name := "tfacc-" + strings.ToLower(acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckImageDestroyed(t, name),
		Steps: []resource.TestStep{
			{
				Config: imageConfig(name, "1Gi"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_rbd_image.test", "id", name),
					resource.TestCheckResourceAttr("homelab_rbd_image.test", "name", name),
					resource.TestCheckResourceAttr("homelab_rbd_image.test", "size", "1Gi"),
				),
			},
			{
				ResourceName:      "homelab_rbd_image.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			{
				// Grow in place.
				Config: imageConfig(name, "2Gi"),
				Check:  resource.TestCheckResourceAttr("homelab_rbd_image.test", "size", "2Gi"),
			},
			{
				// Shrink must be rejected.
				Config:      imageConfig(name, "1Gi"),
				ExpectError: regexp.MustCompile("refusing to shrink"),
			},
		},
	})
}

func testAccCheckImageDestroyed(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveClient(t).Read(name)
		if err == nil {
			return fmt.Errorf("rbd image %q still exists after destroy", name)
		}
		if !rbdimage.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed image %q: %v", name, err)
		}
		return nil
	}
}
