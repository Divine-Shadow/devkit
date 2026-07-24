package productsourceacquire

import "time"

const (
	ManifestSchema = "devkit/product-source-acquisition-manifest/v1"
	ReceiptSchema  = "devkit/product-source-acquisition-receipt/v1"
)

type Manifest struct {
	SchemaVersion  string           `json:"schemaVersion"`
	PackagePath    string           `json:"packagePath"`
	ExecutablePath string           `json:"executablePath"`
	Product        ProductBinding   `json:"product"`
	Runtime        RuntimeBinding   `json:"runtime"`
	Transport      TransportBinding `json:"transport"`
}

type ProductBinding struct {
	Origin          string `json:"origin"`
	Revision        string `json:"revision"`
	LifecycleRoot   string `json:"lifecycleRoot"`
	CheckoutPath    string `json:"checkoutPath"`
	ReceiptPath     string `json:"receiptPath"`
	TaintMarkerPath string `json:"taintMarkerPath"`
	IdentityPath    string `json:"identityPath"`
}

type RuntimeBinding struct {
	GitExecutablePath     string `json:"gitExecutablePath"`
	OpenSSHExecutablePath string `json:"openSSHExecutablePath"`
	Path                  string `json:"path"`
}

type TransportBinding struct {
	SchemaVersion        string `json:"schemaVersion"`
	NetworkMode          string `json:"networkMode"`
	ExecutablePath       string `json:"executablePath"`
	GitSSHExecutablePath string `json:"gitSSHExecutablePath"`
	SSHConfigPath        string `json:"sshConfigPath"`
	KnownHostsPath       string `json:"knownHostsPath"`
	AllowlistPath        string `json:"allowlistPath"`
	NetworkContractPath  string `json:"networkContractPath"`
	ManagedConnectProxy  string `json:"managedConnectProxy"`
	SocketPath           string `json:"socketPath"`
}

type Failure struct {
	Stage string `json:"stage"`
	Code  string `json:"code"`
}

type ContentIdentity struct {
	Kind     string `json:"kind"`
	Revision string `json:"revision"`
	Tree     string `json:"tree"`
}

type LocalGitFlakeInput struct {
	URL      string `json:"url"`
	Revision string `json:"revision"`
	Tree     string `json:"tree"`
}

type TeardownOwnership struct {
	Owner  string `json:"owner"`
	Action string `json:"action"`
}

type OwnedRuntimeStop struct {
	TransportProcessStopped bool `json:"transportProcessStopped"`
	TransportSocketStopped  bool `json:"transportSocketStopped"`
}

type Receipt struct {
	SchemaVersion          string              `json:"schemaVersion"`
	Status                 string              `json:"status"`
	Failure                *Failure            `json:"failure,omitempty"`
	ManifestPath           string              `json:"manifestPath"`
	ManifestSHA256         string              `json:"manifestSha256,omitempty"`
	ProductOrigin          string              `json:"productOrigin,omitempty"`
	ProductRevision        string              `json:"productRevision,omitempty"`
	LifecycleRoot          string              `json:"lifecycleRoot,omitempty"`
	CheckoutPath           string              `json:"checkoutPath,omitempty"`
	ReceiptPath            string              `json:"receiptPath,omitempty"`
	RootOwnershipAcquired  bool                `json:"rootOwnershipAcquired"`
	Tainted                bool                `json:"tainted"`
	TaintMarkerWritten     bool                `json:"taintMarkerWritten"`
	ReceiptWritten         bool                `json:"receiptWritten"`
	OriginVerified         bool                `json:"originVerified"`
	HeadVerified           bool                `json:"headVerified"`
	CleanWorktreeVerified  bool                `json:"cleanWorktreeVerified"`
	GitMetadataWriteProved bool                `json:"gitMetadataWriteProved"`
	ContentIdentity        *ContentIdentity    `json:"contentIdentity,omitempty"`
	LocalGitFlakeInput     *LocalGitFlakeInput `json:"localGitFlakeInput,omitempty"`
	OwnedRuntimeStop       OwnedRuntimeStop    `json:"ownedRuntimeStop"`
	Teardown               TeardownOwnership   `json:"teardown"`
	CompletedAtUTC         string              `json:"completedAtUtc"`
}

func newReceipt(manifestPath string) Receipt {
	return Receipt{
		SchemaVersion: ReceiptSchema,
		Status:        "failure",
		ManifestPath:  manifestPath,
		Teardown: TeardownOwnership{
			Owner:  "outer-wsl-lifecycle",
			Action: "destroy-entire-lifecycle-root-after-failure-or-final-agent-teardown",
		},
	}
}

func (receipt *Receipt) completeClock() {
	if receipt.CompletedAtUTC == "" {
		receipt.CompletedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}
}
