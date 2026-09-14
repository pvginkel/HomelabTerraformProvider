package s3reader_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

var s3EnvVars = []string{
	"HOMELAB_S3_ENDPOINT",
	"HOMELAB_S3_ADMIN_ACCESS_KEY",
	"HOMELAB_S3_ADMIN_SECRET_KEY",
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range s3EnvVars {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

// liveAdmin reads the user record the resource does not expose (caps,
// max_buckets).
func liveAdmin(t *testing.T) *admin.API {
	t.Helper()
	api, err := admin.New(
		os.Getenv("HOMELAB_S3_ENDPOINT"),
		os.Getenv("HOMELAB_S3_ADMIN_ACCESS_KEY"),
		os.Getenv("HOMELAB_S3_ADMIN_SECRET_KEY"),
		&http.Client{},
	)
	if err != nil {
		t.Fatalf("build rgw admin client: %v", err)
	}
	return api
}

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homelab": providerserver.NewProtocol6WithError(provider.New("test")()),
}
