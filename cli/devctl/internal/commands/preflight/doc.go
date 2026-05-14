// Package preflight implements the "preflight" host diagnostics command.
// It checks required native runtime tools, reports optional brokered Docker
// availability, validates ~/.codex contents, and performs optional credential
// pool verification when enabled via env.
package preflight
