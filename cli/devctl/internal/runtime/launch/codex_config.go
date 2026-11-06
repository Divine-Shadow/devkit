package launch

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This recognizes only the Codex-managed project trust extension to an exact
// immutable selected GUI config. It does not merge or interpret base settings.
// Unknown extensions retain the seeder's ordinary source replacement behavior.
func matchesCodexManagedProjectTrustTail(data, source []byte) bool {
	if len(source) == 0 || source[len(source)-1] != '\n' || !bytes.HasPrefix(data, source) {
		return false
	}
	projects := map[string]bool{}
	for _, raw := range strings.Split(string(source), "\n") {
		line := strings.Trim(strings.TrimSuffix(raw, "\r"), " \t")
		if !strings.HasPrefix(line, "[projects.") {
			continue
		}
		project, ok := codexManagedProjectSection(line)
		if !ok || projects[project] {
			return false
		}
		projects[project] = true
	}
	active, hasTrust := false, false
	for _, raw := range strings.Split(string(data[len(source):]), "\n") {
		line := strings.Trim(strings.TrimSuffix(raw, "\r"), " \t")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if active && !hasTrust {
				return false
			}
			project, ok := codexManagedProjectSection(line)
			if !ok || projects[project] {
				return false
			}
			projects[project] = true
			active, hasTrust = true, false
			continue
		}
		if !active || hasTrust {
			return false
		}
		key, value, ok := strings.Cut(line, "=")
		value = strings.Trim(value, " \t")
		if !ok || strings.Trim(key, " \t") != "trust_level" || (value != "\"trusted\"" && value != "\"untrusted\"") {
			return false
		}
		hasTrust = true
	}
	return active && hasTrust
}

func codexManagedProjectSection(section string) (string, bool) {
	if !strings.HasPrefix(section, "[projects.") || !strings.HasSuffix(section, "]") {
		return "", false
	}
	quoted := strings.TrimSuffix(strings.TrimPrefix(section, "[projects."), "]")
	if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' || !utf8.ValidString(quoted) {
		return "", false
	}
	// strconv decodes the common TOML basic-string escapes, but also accepts
	// Go-only escapes. Exclude those before decoding this closed key syntax.
	for i := 1; i < len(quoted)-1; i++ {
		if quoted[i] < 0x20 || quoted[i] == 0x7f {
			return "", false
		}
		if quoted[i] != '\\' {
			continue
		}
		i++
		if i >= len(quoted)-1 || !strings.ContainsRune("\"\\btnfruU", rune(quoted[i])) {
			return "", false
		}
	}
	project, err := strconv.Unquote(quoted)
	return project, err == nil && utf8.ValidString(project) && filepath.IsAbs(project) &&
		filepath.Clean(project) == project && strings.IndexFunc(project, unicode.IsControl) < 0
}
