package s3storage

// Storage is one S3 allocation as the provider sees it: the RGW user (== the
// logical name), its minted credential, and the buckets it owns.
type Storage struct {
	Name            string
	AccessKeyID     string
	SecretAccessKey string
	Buckets         []string
}
