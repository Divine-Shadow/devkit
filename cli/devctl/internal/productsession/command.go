package productsession

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	PrepareToken  = "devkit-product-prepare/v1"
	CommandSchema = "devkit/product-ssh-command/v1"
)

type Kind string

const (
	KindPrepare Kind = "prepare"
	KindListen  Kind = "app-server-listen"
	KindProxy   Kind = "app-server-proxy"
	KindVersion Kind = "version"
)

type Command struct {
	SchemaVersion string   `json:"schema_version"`
	Kind          Kind     `json:"kind"`
	OriginalSHA   string   `json:"original_sha256"`
	Normalized    []string `json:"normalized_argv"`
	CodexConfig   []string `json:"codex_config_argv,omitempty"`
}

// ParseOriginalCommand recognizes a deliberately tiny, non-shell grammar.
// Codex Desktop 0.144 places one package-approved -c assignment before the
// app-server subcommand. Normalization happens before subcommand matching so
// that assignment cannot bypass the one managed target. Quotes, escapes,
// expansions, paths, aliases, and caller-selected sockets are not grammar.
func ParseOriginalCommand(original string) (Command, error) {
	digest := sha256.Sum256([]byte(original))
	result := Command{
		SchemaVersion: CommandSchema,
		OriginalSHA:   hex.EncodeToString(digest[:]),
	}
	if original == PrepareToken {
		result.Kind = KindPrepare
		result.Normalized = []string{PrepareToken}
		return result, nil
	}
	fields, err := exactFields(original)
	if err != nil {
		return Command{}, err
	}
	if len(fields) < 2 || fields[0] != "codex" {
		return Command{}, fmt.Errorf("Product SSH command requires the package-owned codex grammar")
	}
	fields = fields[1:]
	configurationSeen := false
	if len(fields) >= 2 && fields[0] == "-c" {
		if fields[1] != "features.code_mode_host=true" {
			return Command{}, fmt.Errorf("Product SSH command refuses unapproved Codex configuration")
		}
		configurationSeen = true
		result.CodexConfig = []string{"-c", "features.code_mode_host=true"}
		fields = fields[2:]
	}
	for _, field := range fields {
		if field == "-c" || strings.HasPrefix(field, "-c=") {
			return Command{}, fmt.Errorf("Product SSH command refuses duplicate or misplaced Codex configuration")
		}
	}
	switch {
	case len(fields) == 1 && fields[0] == "--version" && !configurationSeen:
		result.Kind = KindVersion
		result.Normalized = []string{"codex", "--version"}
	case len(fields) == 3 && fields[0] == "app-server" && fields[1] == "--listen" && fields[2] == "unix://":
		result.Kind = KindListen
		result.Normalized = append([]string{"codex"}, result.CodexConfig...)
		result.Normalized = append(result.Normalized, "app-server", "managed-listen")
	case len(fields) == 2 && fields[0] == "app-server" && fields[1] == "proxy":
		result.Kind = KindProxy
		result.Normalized = append([]string{"codex"}, result.CodexConfig...)
		result.Normalized = append(result.Normalized, "app-server", "managed-proxy")
	default:
		return Command{}, fmt.Errorf("Product SSH command is outside the exact prepare/version/app-server grammar")
	}
	return result, nil
}

func exactFields(original string) ([]string, error) {
	if original == "" || strings.TrimSpace(original) != original {
		return nil, fmt.Errorf("Product SSH command is empty or not byte-canonical")
	}
	for _, value := range []byte(original) {
		if value < 0x20 || value > 0x7e || value == '\'' || value == '"' || value == '\\' || value == '$' || value == '`' || value == ';' || value == '|' || value == '&' || value == '<' || value == '>' {
			return nil, fmt.Errorf("Product SSH command contains shell syntax or control bytes")
		}
	}
	fields := strings.Split(original, " ")
	for _, field := range fields {
		if field == "" {
			return nil, fmt.Errorf("Product SSH command spacing is not byte-canonical")
		}
	}
	return fields, nil
}
