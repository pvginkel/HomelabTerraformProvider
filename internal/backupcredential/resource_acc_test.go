package backupcredential_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pvginkel/HomelabTerraformProvider/internal/backupcredential"
)

func liveClient() *backupcredential.Client {
	return backupcredential.NewClient(
		os.Getenv("HOMELAB_BACKUP_SERVER_URL"),
		os.Getenv("HOMELAB_BACKUP_SERVER_TOKEN"),
		"acctest",
	)
}

// credentialConfig builds a single-resource HCL config. Provider URL/token
// come from the env vars TestAccProtoV6ProviderFactories already plumbs through.
func credentialConfig(scope string, retention int) string {
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_backup_credential" "test" {
  scope     = %q
  retention = %d
}
`, scope, retention)
}

func TestAccBackupCredential_basic(t *testing.T) {
	scope := "tfacc-" + strings.ToLower(acctest.RandString(8))

	var capturedToken string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCredentialDestroyed(scope),
		Steps: []resource.TestStep{
			{
				Config: credentialConfig(scope, 10),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_backup_credential.test", "id", scope),
					resource.TestCheckResourceAttr("homelab_backup_credential.test", "scope", scope),
					resource.TestCheckResourceAttr("homelab_backup_credential.test", "retention", "10"),
					resource.TestCheckResourceAttrSet("homelab_backup_credential.test", "token"),
					captureAttr("homelab_backup_credential.test", "token", &capturedToken),
				),
			},
			{
				ResourceName:      "homelab_backup_credential.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: credentialConfig(scope, 25),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_backup_credential.test", "retention", "25"),
					checkAttrEqualsCaptured("homelab_backup_credential.test", "token", &capturedToken),
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
		v, ok := rs.Primary.Attributes[attr]
		if !ok {
			return fmt.Errorf("attribute %s not in state for %s", attr, resourceName)
		}
		*dst = v
		return nil
	}
}

func checkAttrEqualsCaptured(resourceName, attr string, captured *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		if *captured == "" {
			return fmt.Errorf("captured value is empty; previous step did not populate it")
		}
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resourceName)
		}
		got := rs.Primary.Attributes[attr]
		if got != *captured {
			return fmt.Errorf("%s.%s = %q, want %q (preserved across update)", resourceName, attr, got, *captured)
		}
		return nil
	}
}

func testAccCheckCredentialDestroyed(scope string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveClient().Get(context.Background(), scope)
		if err == nil {
			return fmt.Errorf("credential %q still exists after destroy", scope)
		}
		if !backupcredential.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed credential %q: %v", scope, err)
		}
		return nil
	}
}
