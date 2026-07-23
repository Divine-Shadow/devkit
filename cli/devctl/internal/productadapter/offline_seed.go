package productadapter

import "path/filepath"

const (
	OfflineSeedSchema         = "devkit/product-stopped-volume-seed/v1"
	OfflineProjectionSchema   = "devkit/product-relative-root-projection/v1"
	SourceIdentityEnvironment = "DEVKIT_SOURCE_TRANSPORT_IDENTITY"
)

type OfflineSeedMarker struct {
	SchemaVersion        string `json:"schema_version"`
	ConsumerIndex        int    `json:"consumer_index"`
	CandidateRoot        string `json:"candidate_root"`
	AuthorizedKeysSHA256 string `json:"authorized_keys_sha256"`
}

func OfflineSeedMarkerPath(consumer ConsumerManifest) string {
	return filepath.Join(consumer.CandidateRoot, ".devkit-product-offline-seed.json")
}
