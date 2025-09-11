package files

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendLineIfMissing appends a line to a text file if it's not already present, using atomic write.
func AppendLineIfMissing(path, line string) (added bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, errors.New("empty line")
	}
	var existing []string
	if data, err := os.ReadFile(path); err == nil {
		s := bufio.NewScanner(strings.NewReader(string(data)))
		for s.Scan() {
			existing = append(existing, strings.TrimRight(s.Text(), "\r\n"))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	for _, l := range existing {
		if l == line {
			return false, nil
		}
	}
	existing = append(existing, line)
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	for i, l := range existing {
		if i > 0 {
			if _, err := f.WriteString("\n"); err != nil {
				return false, err
			}
		}
		if _, err := f.WriteString(l); err != nil {
			return false, err
		}
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}
