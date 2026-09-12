package worktrees

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InspectNativeLaneGitMetadata validates the existing host registration without
// migrating it. SetupNative continues to own declared-origin admission. This
// inspector binds the projection only to the selected package-owned lane (or
// its existing legacy common), and never repairs incomplete or conflicting data.
func InspectNativeLaneGitMetadata(wt, root, repo string, index int) (NativeGitMetadata, error) {
	expected, err := nativeOwnedCommonRepositoryPath(root, repo, index)
	if err != nil {
		return NativeGitMetadata{}, err
	}
	legacy, err := nativeLegacyOwnedCommonRepositoryPath(root, repo)
	if err != nil {
		return NativeGitMetadata{}, err
	}
	common, err := nativeWorktreeCommonDirectory(wt)
	if err != nil {
		return NativeGitMetadata{}, err
	}
	if common != expected && common != legacy {
		return NativeGitMetadata{}, fmt.Errorf("native projection rejects foreign common repository %s", common)
	}
	metadata, err := inspectNativeGitMetadata(wt, root, common)
	if err != nil {
		return NativeGitMetadata{}, err
	}
	if filepath.Clean(wt) != filepath.Join(filepath.Clean(root), fmt.Sprintf("agent%d", index), repo) || metadata.Worktree != filepath.Clean(wt) {
		return NativeGitMetadata{}, fmt.Errorf("native projection requires exact selected lane geometry")
	}

	if filepath.Dir(metadata.GitDir) != filepath.Join(common, "worktrees") {
		return NativeGitMetadata{}, fmt.Errorf("native projection requires one direct worktree registration")
	}
	// Do not let canonicalization hide a symlink or traversal in a metadata
	// pointer. Relative .. components remain legal only at their exact targets.
	for _, path := range []string{root, wt, common, metadata.GitDir} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != filepath.Clean(path) {
			return NativeGitMetadata{}, fmt.Errorf("native projection rejects symlinked metadata geometry %s", path)
		}
	}
	for _, path := range []string{metadata.GitFile, filepath.Join(metadata.GitDir, "commondir"), filepath.Join(metadata.GitDir, "gitdir"), filepath.Join(common, "devkit-owned-common")} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return NativeGitMetadata{}, fmt.Errorf("native projection requires regular metadata file %s", path)
		}
	}
	forward, err := readGitdirPointer(metadata.GitFile)
	if err != nil {
		return NativeGitMetadata{}, err
	}
	if filepath.IsAbs(forward) || filepath.Clean(filepath.Join(wt, forward)) != metadata.GitDir {
		return NativeGitMetadata{}, fmt.Errorf("native projection requires relative exact gitdir")
	}
	commondir, err := readPlainMetadataPath(filepath.Join(metadata.GitDir, "commondir"))
	if err != nil {
		return NativeGitMetadata{}, err
	}
	if filepath.IsAbs(commondir) || filepath.Clean(filepath.Join(metadata.GitDir, commondir)) != common {
		return NativeGitMetadata{}, fmt.Errorf("native projection requires relative exact commondir")
	}
	marker, err := os.ReadFile(filepath.Join(common, "devkit-owned-common"))
	if err != nil {
		return NativeGitMetadata{}, err
	}
	lines := strings.Split(string(marker), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[2], "origin=") {
		return NativeGitMetadata{}, fmt.Errorf("native projection requires package-owned common marker")
	}
	origin := strings.TrimPrefix(lines[2], "origin=")
	wanted, err := nativeOwnedCommonRepositoryMarker(repo, origin, index)
	if common == legacy {
		wanted, err = nativeLegacyOwnedCommonRepositoryMarker(repo, origin)
	}
	if err != nil || string(marker) != wanted {
		return NativeGitMetadata{}, fmt.Errorf("native projection common marker does not identify selected lane")
	}
	// The selected .git and its reverse pointer identify one reciprocal pair.
	// Other registrations cannot change that pair and are not pruning gates.

	return metadata, nil
}
