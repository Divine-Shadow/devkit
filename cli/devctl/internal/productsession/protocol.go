package productsession

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	RequestSchema  = "devkit/product-supervisor-request/v1"
	ResponseSchema = "devkit/product-supervisor-response/v1"
	StatusSchema   = "devkit/product-supervisor-status/v1"
	FailureSchema  = "devkit/product-supervisor-failure/v1"
	maximumFrame   = 64 * 1024
)

type Request struct {
	SchemaVersion   string `json:"schema_version"`
	Count           int    `json:"count"`
	Index           int    `json:"index"`
	OriginalCommand string `json:"original_command"`
}

type Status struct {
	SchemaVersion              string `json:"schema_version"`
	ManifestPath               string `json:"manifest_path"`
	ManifestDigest             string `json:"manifest_digest"`
	ConsumerIndex              int    `json:"consumer_index"`
	MountPolicyIdentity        string `json:"mount_policy_identity"`
	MountPolicyContractPath    string `json:"mount_policy_contract_path"`
	MountPolicyContractDigest  string `json:"mount_policy_contract_digest"`
	MCPRequirementPath         string `json:"mcp_requirement_path"`
	MCPRequirementDigest       string `json:"mcp_requirement_digest"`
	CodexExecutablePath        string `json:"codex_executable_path"`
	CodexExecutableDigest      string `json:"codex_executable_digest"`
	SupervisorPID              int    `json:"supervisor_pid"`
	SupervisorStartTime        uint64 `json:"supervisor_start_time"`
	ControlSocketPath          string `json:"control_socket_path"`
	AppServerRunning           bool   `json:"app_server_running"`
	AppServerPID               int    `json:"app_server_pid,omitempty"`
	AppServerStartTime         uint64 `json:"app_server_start_time,omitempty"`
	AppServerProcessGroup      int    `json:"app_server_process_group,omitempty"`
	AppServerSocketPath        string `json:"app_server_socket_path"`
	SandboxAppServerSocketPath string `json:"sandbox_app_server_socket_path"`
	AppServerSocketDevice      uint64 `json:"app_server_socket_device,omitempty"`
	AppServerSocketInode       uint64 `json:"app_server_socket_inode,omitempty"`
	AppServerSocketOwner       uint32 `json:"app_server_socket_owner,omitempty"`
	AppServerKernelSocketInode uint64 `json:"app_server_kernel_socket_inode,omitempty"`
	AppServerProcessCount      int    `json:"app_server_process_count"`
	MountNamespaceDistinct     bool   `json:"mount_namespace_distinct"`
	NetworkNamespaceDistinct   bool   `json:"network_namespace_distinct"`
	WindowsMountsAbsent        bool   `json:"windows_mounts_absent"`
	StartCount                 int    `json:"start_count"`
}

type Response struct {
	SchemaVersion string   `json:"schema_version"`
	Command       Command  `json:"command"`
	Status        Status   `json:"status"`
	Output        string   `json:"output,omitempty"`
	Failure       *Failure `json:"failure,omitempty"`
}

type Failure struct {
	SchemaVersion string `json:"schema_version"`
	Phase         string `json:"phase"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

type FailureOperation string

const (
	FailureRequest FailureOperation = "request"
	FailurePrepare FailureOperation = "consumer-prepare"
	FailureListen  FailureOperation = "app-server-start"
	FailureProxy   FailureOperation = "app-server-proxy"
	FailureVersion FailureOperation = "codex-version"
)

func FailureOperationFor(command Command) FailureOperation {
	switch command.Kind {
	case KindPrepare:
		return FailurePrepare
	case KindListen:
		return FailureListen
	case KindProxy:
		return FailureProxy
	case KindVersion:
		return FailureVersion
	default:
		return FailureRequest
	}
}

func FailureFor(operation FailureOperation, cause error) *Failure {
	if cause == nil {
		return nil
	}
	code := ""
	switch operation {
	case FailureRequest:
		code = "invalid-request"
	case FailurePrepare:
		code = "consumer-preparation-failed"
	case FailureListen:
		code = "app-server-start-failed"
	case FailureProxy:
		code = "app-server-proxy-failed"
	case FailureVersion:
		code = "codex-version-failed"
	default:
		return nil
	}
	return &Failure{
		SchemaVersion: FailureSchema,
		Phase:         string(operation),
		Code:          code,
		Message:       cause.Error(),
	}
}

func (failure *Failure) Valid() bool {
	if failure == nil || failure.SchemaVersion != FailureSchema ||
		failure.Phase == "" || failure.Code == "" || failure.Message == "" {
		return false
	}
	validPairs := map[string]string{
		string(FailureRequest): "invalid-request",
		string(FailurePrepare): "consumer-preparation-failed",
		string(FailureListen):  "app-server-start-failed",
		string(FailureProxy):   "app-server-proxy-failed",
		string(FailureVersion): "codex-version-failed",
	}
	return validPairs[failure.Phase] == failure.Code
}

func WriteFrame(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maximumFrame {
		return fmt.Errorf("Product supervisor frame exceeds the bounded size")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

func ReadFrame(reader io.Reader, destination any) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maximumFrame {
		return fmt.Errorf("Product supervisor frame size is invalid")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytesReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("Product supervisor frame contains trailing JSON")
	}
	return nil
}

// bytesReader avoids a buffered transport wrapper: a proxy request is
// followed by raw app-server bytes on the same connection.
func bytesReader(payload []byte) io.Reader {
	return &sliceReader{payload: payload}
}

type sliceReader struct {
	payload []byte
}

func (reader *sliceReader) Read(target []byte) (int, error) {
	if len(reader.payload) == 0 {
		return 0, io.EOF
	}
	count := copy(target, reader.payload)
	reader.payload = reader.payload[count:]
	return count, nil
}
