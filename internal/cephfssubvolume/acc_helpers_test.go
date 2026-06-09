package cephfssubvolume_test

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pvginkel/HomelabTerraformProvider/internal/cephconn"
	"github.com/pvginkel/HomelabTerraformProvider/internal/cephfssubvolume"
	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

var cephEnvVars = []string{
	"HOMELAB_CEPH_MON_HOST",
	"HOMELAB_CEPH_USER",
	"HOMELAB_CEPH_KEY",
	"HOMELAB_CEPH_POOL",
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range cephEnvVars {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

func liveClient(t *testing.T) *cephfssubvolume.Client {
	t.Helper()
	conn, err := cephconn.New(
		os.Getenv("HOMELAB_CEPH_MON_HOST"),
		os.Getenv("HOMELAB_CEPH_USER"),
		os.Getenv("HOMELAB_CEPH_KEY"),
		os.Getenv("HOMELAB_CEPH_POOL"),
	)
	if err != nil {
		t.Fatalf("connect to ceph: %v", err)
	}
	t.Cleanup(conn.Close)
	return cephfssubvolume.NewClient(conn)
}

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homelab": providerserver.NewProtocol6WithError(provider.New("test")()),
}
