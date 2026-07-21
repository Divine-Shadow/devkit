package productadapter

import "path/filepath"

const (
	OfflineSeedSchema         = "devkit/product-stopped-volume-seed/v1"
	OfflineProjectionSchema   = "devkit/product-relative-root-projection/v1"
	SourceIdentityEnvironment = "DEVKIT_SOURCE_TRANSPORT_IDENTITY"
)

func OfflineSeedMarkerPath(consumer ConsumerManifest) string {
	return filepath.Join(consumer.CandidateRoot, ".devkit-product-offline-seed.json")
}
