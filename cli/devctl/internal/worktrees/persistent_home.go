package worktrees

import (
	runtimeagent "devkit/cli/devctl/internal/runtime/agent"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// These obligations belong to the opaque reset plan. A wildcard is used only
// for the repository component of agent1's legacy home, since whole-prefix
// disposal covers every repository in that lane.
type nativeResetPathPattern struct {
	path                     string
	wildcardComponent        int
	allowEmptyHomeMountpoint bool
}

func nativeResetPathParts(path string) []string {
	return strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
}

func nativeResetLiteralPath(path string) nativeResetPathPattern {
	return nativeResetPathPattern{path: path, wildcardComponent: -1}
}

func nativeResetRepositoryPattern(agentRoot, suffix string) nativeResetPathPattern {
	return nativeResetPathPattern{
		path: filepath.Join(agentRoot, "*", suffix), wildcardComponent: len(nativeResetPathParts(agentRoot)),
	}
}

func (pattern nativeResetPathPattern) matchesPrefix(actual []string) bool {
	parts := nativeResetPathParts(pattern.path)
	for index := 0; index < len(parts) && index < len(actual); index++ {
		if index != pattern.wildcardComponent && parts[index] != actual[index] {
			return false
		}
	}
	return true
}

func nativeResetLegacyHomePatterns(agentRoot, stateRoot, repo string, index int, whole bool) []nativeResetPathPattern {
	var home nativeResetPathPattern
	if index == 1 {
		if whole {
			home = nativeResetRepositoryPattern(agentRoot, ".devhome-agent1")
		} else {
			home = nativeResetLiteralPath(filepath.Join(agentRoot, repo, ".devhome-agent1"))
		}
	} else {
		home = nativeResetLiteralPath(filepath.Join(agentRoot, fmt.Sprintf(".devhome-agent%d", index)))
	}
	home.allowEmptyHomeMountpoint = true
	return []nativeResetPathPattern{home, nativeResetLiteralPath(filepath.Join(stateRoot, "home"))}
}

// projectNativeResetPattern follows the existing source-to-quarantine mapping.
// If the protected home itself (or a descendant) was moved, its entire target
// remains protected even though the numbered target has lost the home's name.
func projectNativeResetPattern(pattern nativeResetPathPattern, source, target string) (nativeResetPathPattern, bool) {
	parts := nativeResetPathParts(pattern.path)
	sourceParts := nativeResetPathParts(source)
	if !pattern.matchesPrefix(sourceParts) {
		return nativeResetPathPattern{}, false
	}
	if len(sourceParts) >= len(parts) {
		result := nativeResetLiteralPath(target)
		result.allowEmptyHomeMountpoint = pattern.allowEmptyHomeMountpoint && len(sourceParts) == len(parts)
		return result, true
	}
	result := nativeResetLiteralPath(filepath.Join(append([]string{target}, parts[len(sourceParts):]...)...))
	result.allowEmptyHomeMountpoint = pattern.allowEmptyHomeMountpoint
	if pattern.wildcardComponent >= len(sourceParts) {
		result.wildcardComponent = len(nativeResetPathParts(target)) + pattern.wildcardComponent - len(sourceParts)
	}
	return result, true
}

// visitNativeResetPattern enumerates the single source-derived wildcard without
// following payload symlinks. Unlike filepath.Glob, filesystem errors are not
// discarded: unreadable protected-data geometry refuses deletion.
func visitNativeResetPattern(pattern nativeResetPathPattern, visit func(string) error) error {
	parts := nativeResetPathParts(pattern.path)
	var inspect func(string, []string, int) error
	inspect = func(parent string, remaining []string, position int) error {
		if len(remaining) == 0 {
			return visit(parent)
		}
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect native reset protected-data parent %s: %w", parent, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if position != pattern.wildcardComponent {
			return inspect(filepath.Join(parent, remaining[0]), remaining[1:], position+1)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			return fmt.Errorf("enumerate native reset legacy repository homes %s: %w", parent, err)
		}
		for _, entry := range entries {
			if err := inspect(filepath.Join(parent, entry.Name()), remaining[1:], position+1); err != nil {
				return err
			}
		}
		return nil
	}
	return inspect(string(filepath.Separator), parts, 0)
}

func (plan *NativeResetPlan) requireLegacyHomesAbsent(state NativeResetTransactionState) error {
	for _, pattern := range plan.legacyHomePatterns {
		patterns := []nativeResetPathPattern{pattern}
		for _, item := range state.Staged {
			if mapped, ok := projectNativeResetPattern(pattern, item.Source, item.Target); ok {
				patterns = append(patterns, mapped)
			}
		}
		for _, current := range patterns {
			if err := visitNativeResetPattern(current, func(path string) error {
				_, err := os.Lstat(path)
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if err != nil {
					return fmt.Errorf("inspect legacy native home %s: %w", path, err)
				}
				if current.allowEmptyHomeMountpoint {
					empty, err := runtimeagent.EmptyNativeHomeMountpoint(path)
					if err != nil {
						return fmt.Errorf("inspect legacy native home %s: %w", path, err)
					}
					if empty {
						return nil
					}
				}
				return fmt.Errorf("legacy native home %s requires environment-owned migration to persistent storage before reset; data was left unchanged", path)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type nativeResetQuarantineCheck struct {
	boundary      nativeResetPathPattern
	prefix        string
	canonicalOnly bool
}

// A name is not proof that opaque old payload is disposable. Only an existing
// structurally validated transaction supplies its source-to-target mapping.
// Ordinary fresh staging refuses residue; recovery may still restore it, or
// commit it after the mapped protected-data obligations pass.
func (plan *NativeResetPlan) requireMappedQuarantines(state NativeResetTransactionState, includeActiveResidue bool) error {
	// Staging must classify everything it is about to move. Commit owns only
	// its retained mapping: unrelated residue outside that cleanup cannot block
	// or acquire disposal authority from an already admitted transaction.
	known := make(map[string]bool, len(state.Quarantines))
	for _, quarantine := range state.Quarantines {
		known[quarantine.Path] = true
	}
	insideCleanup := func(path string) bool {
		for _, quarantine := range state.Quarantines {
			if pathWithinRoot(quarantine.Path, path) {
				return true
			}
		}
		return false
	}
	for _, check := range plan.quarantineChecks {
		boundaries := []nativeResetPathPattern{check.boundary}
		for _, item := range state.Staged {
			parentParts := nativeResetPathParts(filepath.Dir(item.Source))
			parentMatches := len(parentParts) == len(nativeResetPathParts(check.boundary.path)) && check.boundary.matchesPrefix(parentParts)
			name := filepath.Base(item.Source)
			matches := strings.HasPrefix(name, check.prefix) || nativeResetLegacyQuarantineName(name)
			if check.canonicalOnly {
				matches = nativeResetCanonicalTransactionQuarantineName(name, check.prefix)
			}
			if parentMatches && matches && !known[item.Source] {
				return fmt.Errorf("native reset quarantine payload %s lacks its original transaction recovery mapping; data was left unchanged", item.Target)
			}
			if len(nativeResetPathParts(item.Source)) <= len(nativeResetPathParts(check.boundary.path)) {
				if mapped, ok := projectNativeResetPattern(check.boundary, item.Source, item.Target); ok {
					boundaries = append(boundaries, mapped)
				}
			}
		}
		for _, boundary := range boundaries {
			if err := visitNativeResetPattern(boundary, func(path string) error {
				info, err := os.Lstat(path)
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if err != nil {
					return err
				}
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return nil
				}
				entries, err := os.ReadDir(path)
				if err != nil {
					return fmt.Errorf("inspect native reset quarantine boundary %s: %w", path, err)
				}
				for _, entry := range entries {
					name := entry.Name()
					matches := strings.HasPrefix(name, check.prefix) || nativeResetLegacyQuarantineName(name)
					if check.canonicalOnly {
						matches = nativeResetCanonicalTransactionQuarantineName(name, check.prefix)
					}
					candidate := filepath.Join(path, name)
					if matches && !known[candidate] && (includeActiveResidue || insideCleanup(candidate)) {
						return fmt.Errorf("native reset quarantine %s requires its source-derived transaction recovery mapping; opaque residue was left unchanged", candidate)
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (plan *NativeResetPlan) validatePersistentData(state NativeResetTransactionState, includeActiveResidue bool) error {
	if err := plan.requireLegacyHomesAbsent(state); err != nil {
		return err
	}
	return plan.requireMappedQuarantines(state, includeActiveResidue)
}
