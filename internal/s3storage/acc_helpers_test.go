package s3storage_test

import (
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
	"github.com/pvginkel/HomelabTerraformProvider/internal/s3storage"
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

func liveClient(t *testing.T) *s3storage.Client {
	t.Helper()
	client, err := s3storage.NewClient(
		os.Getenv("HOMELAB_S3_ENDPOINT"),
		os.Getenv("HOMELAB_S3_ADMIN_ACCESS_KEY"),
		os.Getenv("HOMELAB_S3_ADMIN_SECRET_KEY"),
		"",
		"acctest",
	)
	if err != nil {
		t.Fatalf("build s3 client: %v", err)
	}
	return client
}

type keyPair struct {
	access, secret string
}

// liveS3 is an S3 client signing with key, for acting as an owner or reader.
func liveS3(key keyPair) *s3.Client {
	return s3.New(s3.Options{
		Region:                     "us-east-1",
		Credentials:                credentials.NewStaticCredentialsProvider(key.access, key.secret, ""),
		BaseEndpoint:               aws.String(os.Getenv("HOMELAB_S3_ENDPOINT")),
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
}

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homelab": providerserver.NewProtocol6WithError(provider.New("test")()),
}
