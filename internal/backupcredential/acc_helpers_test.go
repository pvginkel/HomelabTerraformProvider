package backupcredential_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("HOMELAB_BACKUP_SERVER_URL") == "" {
		t.Fatal("HOMELAB_BACKUP_SERVER_URL must be set for acceptance tests")
	}
	if os.Getenv("HOMELAB_BACKUP_SERVER_TOKEN") == "" {
		t.Fatal("HOMELAB_BACKUP_SERVER_TOKEN must be set for acceptance tests")
	}
}

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homelab": providerserver.NewProtocol6WithError(provider.New("test")()),
}
