package gitutil

// UpdateExcludeScript returns a small script that appends a pattern to
// .git/info/exclude if it's not already present.
func UpdateExcludeScript(repoPath, pattern string) string {
    return "set -e; cd '" + repoPath + "'; gd=$(git rev-parse --git-dir); mkdir -p \"$gd/info\"; (grep -qxF '" + pattern + "' \"$gd/info/exclude\" || echo '" + pattern + "' >> \"$gd/info/exclude\")"
}

