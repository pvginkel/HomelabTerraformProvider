package dnsreservation_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/pvginkel/HomelabTerraformProvider/internal/dnsreservation"
)

const ipv4Pattern = `^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`

func liveClient() *dnsreservation.Client {
	return dnsreservation.NewClient(
		os.Getenv("HOMELAB_DNS_RESERVATION_URL"),
		os.Getenv("HOMELAB_DNS_RESERVATION_TOKEN"),
		"acctest",
	)
}

// reservationConfig builds a single-resource HCL config. Provider URL/token
// come from the env vars TestAccProtoV6ProviderFactories already plumbs through.
func reservationConfig(hostname, mac string) string {
	return fmt.Sprintf(`
provider "homelab" {}

resource "homelab_dns_reservation" "test" {
  hostname = %q
  mac      = %q
}
`, hostname, mac)
}

func TestAccDNSReservation_basic(t *testing.T) {
	hostname := "tfacc-" + strings.ToLower(acctest.RandString(8))
	const macA = "02:00:00:00:00:01"
	const macB = "02:00:00:00:00:02"

	var capturedIPv4 string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckReservationDestroyed(hostname),
		Steps: []resource.TestStep{
			{
				Config: reservationConfig(hostname, macA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_dns_reservation.test", "id", hostname),
					resource.TestCheckResourceAttr("homelab_dns_reservation.test", "hostname", hostname),
					resource.TestCheckResourceAttr("homelab_dns_reservation.test", "mac", macA),
					resource.TestMatchResourceAttr("homelab_dns_reservation.test", "ipv4", regexp.MustCompile(ipv4Pattern)),
					captureAttr("homelab_dns_reservation.test", "ipv4", &capturedIPv4),
				),
			},
			{
				ResourceName:      "homelab_dns_reservation.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: reservationConfig(hostname, macB),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homelab_dns_reservation.test", "mac", macB),
					checkAttrEqualsCaptured("homelab_dns_reservation.test", "ipv4", &capturedIPv4),
				),
			},
		},
	})
}

func TestAccDNSReservation_invalidMAC(t *testing.T) {
	hostname := "tfacc-" + strings.ToLower(acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      reservationConfig(hostname, "aa:bb:cc:dd:ee:ff"),
				ExpectError: regexp.MustCompile("Invalid MAC"),
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

func testAccCheckReservationDestroyed(hostname string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		_, err := liveClient().Get(context.Background(), hostname)
		if err == nil {
			return fmt.Errorf("reservation %q still exists after destroy", hostname)
		}
		if !dnsreservation.IsNotFound(err) {
			return fmt.Errorf("unexpected error checking destroyed reservation %q: %v", hostname, err)
		}
		return nil
	}
}
