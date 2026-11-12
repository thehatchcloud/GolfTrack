package backup

import (
	"os"
	"strings"
)

// The LITESTREAM_* variables production has always set (pocketbase/
// DEPLOYMENT.md). They are the fallback when nothing has been saved on the
// settings page, which keeps an existing deployment backing up exactly as
// before, and lets a server whose volume was lost find its backup again.
const (
	EnvBucket          = "LITESTREAM_BUCKET"
	EnvEndpoint        = "LITESTREAM_ENDPOINT"
	EnvAccessKeyID     = "LITESTREAM_ACCESS_KEY_ID"
	EnvSecretAccessKey = "LITESTREAM_SECRET_ACCESS_KEY"
)

// FromEnv builds the S3 destination described by LITESTREAM_*. ok is false
// when LITESTREAM_BUCKET is unset, the same switch entrypoint.sh used.
//
// It mirrors the old litestream.yml field for field and is not validated
// against the page's rules: whatever production runs with today must keep
// running unchanged.
func FromEnv() (cfg Config, ok bool) {
	bucket := strings.TrimSpace(os.Getenv(EnvBucket))
	if bucket == "" {
		return Config{}, false
	}
	return Config{
		Kind: KindS3,
		S3: &S3Config{
			Provider:        ProviderOther,
			Bucket:          bucket,
			Path:            DefaultPathPrefix,
			Endpoint:        strings.TrimRight(strings.TrimSpace(os.Getenv(EnvEndpoint)), "/"),
			AccessKeyID:     strings.TrimSpace(os.Getenv(EnvAccessKeyID)),
			SecretAccessKey: strings.TrimSpace(os.Getenv(EnvSecretAccessKey)),
		},
	}, true
}
