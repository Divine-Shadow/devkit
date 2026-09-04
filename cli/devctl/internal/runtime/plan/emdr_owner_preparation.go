package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	EMDROwnerPreparationProfileIdentity    = "emdr-owner-preparation/v1"
	EMDROwnerPreparationManifestSchema     = "wsl-nix/emdr-owner-preparation-capability/v1"
	EMDROwnerPreparationIdentitySchema     = "wsl-nix/emdr-owner-preparation-identity/v1"
	EMDROwnerPreparationRequestSchema      = "emdr-owner-preparation/request/v1"
	EMDROwnerPreparationAcceptedSchema     = "emdr-owner-preparation/accepted/v1"
	EMDROwnerPreparationReceiptSchema      = "emdr-owner-preparation/receipt/v1"
	EMDROwnerPreparationHandleEnvironment  = "EMDR_OWNER_PREPARATION_HANDLE"
	EMDROwnerPreparationHandleRequired     = "required"
	EMDROwnerPreparationExpectedSocketMode = "0600"
	EMDROwnerPreparationExpectedIDMode     = "0400"
)

var (
	EMDROwnerPreparationManifestPath  = "/etc/fleet/emdr-owner-preparation-capability.json"
	EMDROwnerPreparationSocketPath    = "/run/fleet-emdr-owner-preparation/control.sock"
	EMDROwnerPreparationIdentityPath  = "/run/fleet-emdr-owner-preparation/identity.json"
	EMDROwnerPreparationExpectedOwner = "bayesartre"
	EMDROwnerPreparationExpectedGroup = "users"
)

type EMDROwnerPreparationCapability struct {
	SchemaVersion    string   `json:"schemaVersion"`
	ProfileIdentity  string   `json:"profileIdentity"`
	Environment      string   `json:"environment"`
	RequiredValue    string   `json:"requiredValue"`
	SocketPath       string   `json:"socketPath"`
	IdentityPath     string   `json:"identityPath"`
	IdentitySchema   string   `json:"identitySchema"`
	SocketMode       string   `json:"socketMode"`
	IdentityMode     string   `json:"identityMode"`
	Owner            string   `json:"owner"`
	Group            string   `json:"group"`
	NoFollow         bool     `json:"noFollow"`
	ClientExecutable string   `json:"clientExecutable"`
	ServerExecutable string   `json:"serverExecutable"`
	RequestSchema    string   `json:"requestSchema"`
	AcceptedSchema   string   `json:"acceptedSchema"`
	ReceiptSchema    string   `json:"receiptSchema"`
	Operations       []string `json:"operations"`
}

func LoadEMDROwnerPreparationCapability(path string) (EMDROwnerPreparationCapability, error) {
	var capability EMDROwnerPreparationCapability
	data, err := os.ReadFile(path)
	if err != nil {
		return capability, fmt.Errorf("read EMDR owner preparation capability %s: %w", path, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&capability); err != nil {
		return capability, fmt.Errorf("decode EMDR owner preparation capability %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return capability, fmt.Errorf("decode EMDR owner preparation capability %s: trailing JSON value", path)
		}
		return capability, fmt.Errorf("decode EMDR owner preparation capability %s: trailing JSON content: %w", path, err)
	}
	if err := validateEMDROwnerPreparationCapability(capability); err != nil {
		return capability, err
	}
	return capability, nil
}

func validateEMDROwnerPreparationCapability(capability EMDROwnerPreparationCapability) error {
	if capability.SchemaVersion != EMDROwnerPreparationManifestSchema ||
		capability.ProfileIdentity != EMDROwnerPreparationProfileIdentity ||
		capability.Environment != EMDROwnerPreparationHandleEnvironment ||
		capability.RequiredValue != EMDROwnerPreparationHandleRequired ||
		filepath.Clean(capability.SocketPath) != filepath.Clean(EMDROwnerPreparationSocketPath) ||
		filepath.Clean(capability.IdentityPath) != filepath.Clean(EMDROwnerPreparationIdentityPath) ||
		capability.IdentitySchema != EMDROwnerPreparationIdentitySchema ||
		capability.SocketMode != EMDROwnerPreparationExpectedSocketMode ||
		capability.IdentityMode != EMDROwnerPreparationExpectedIDMode ||
		capability.Owner != EMDROwnerPreparationExpectedOwner ||
		capability.Group != EMDROwnerPreparationExpectedGroup || !capability.NoFollow ||
		capability.RequestSchema != EMDROwnerPreparationRequestSchema ||
		capability.AcceptedSchema != EMDROwnerPreparationAcceptedSchema ||
		capability.ReceiptSchema != EMDROwnerPreparationReceiptSchema ||
		strings.Join(capability.Operations, "\x00") != "sources\x00jobs\x00status\x00job-create" {
		return fmt.Errorf("EMDR owner preparation capability does not match the compiled fail-closed contract")
	}
	if err := validateControllerStoreExecutable("EMDR owner preparation client", capability.ClientExecutable); err != nil {
		return err
	}
	if err := validateControllerStoreExecutable("EMDR owner preparation server", capability.ServerExecutable); err != nil {
		return err
	}
	return nil
}
