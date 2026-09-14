package s3reader

// Reader is an RGW user that owns no buckets, can create none, and holds only
// the buckets=read admin capability, with its minted credential.
type Reader struct {
	Name            string
	AccessKeyID     string
	SecretAccessKey string
}
