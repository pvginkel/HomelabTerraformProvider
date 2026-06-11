package zfsdataset_test

import (
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
	"github.com/pvginkel/HomelabTerraformProvider/internal/zfsdataset"
)

// Acceptance tests run against a live iac-provisioner agent and a scratch pool.
// Required env:
//   HOMELAB_IAC_PROVISIONER_TOKEN — bearer token for the agent
//   HOMELAB_ZFS_TEST_POOL         — a scratch pool imported on the test node
//   HOMELAB_ZFS_TEST_HOST         — hostname of the node that imports it
//   HOMELAB_IAC_PROVISIONER_PORT  — optional; defaults to 9655

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HOMELAB_IAC_PROVISIONER_TOKEN", "HOMELAB_ZFS_TEST_POOL", "HOMELAB_ZFS_TEST_HOST"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s must be set for acceptance tests", k)
		}
	}
}

func testPool() string { return os.Getenv("HOMELAB_ZFS_TEST_POOL") }
func testHost() string { return os.Getenv("HOMELAB_ZFS_TEST_HOST") }

func testPort() int {
	if v := os.Getenv("HOMELAB_IAC_PROVISIONER_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 9655
}

func liveClient() *zfsdataset.Client {
	return zfsdataset.NewClient(
		map[string]string{testPool(): testHost()},
		os.Getenv("HOMELAB_IAC_PROVISIONER_TOKEN"),
		testPort(),
		"acctest",
	)
}

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homelab": providerserver.NewProtocol6WithError(provider.New("test")()),
}
