package command

import (
	"fmt"
	"path/filepath"
)

// prefixFilePaths prepends the given prefix to the file paths of the test cases.
func prefixPath(path string, prefix string) string {
	if prefix == "" {
		return path
	}

	var prefixedPath string
	// Some test collectors (e.g. Rspec) report file paths with a "./" by default.
	// Since `filepath.Join` ignore "./", we need to handle this case separately to avoid losing the "./" prefix.
	if prefix == "./" {
		prefixedPath = prefix + path
	} else {
		prefixedPath = filepath.Join(prefix, path)
	}
	return prefixedPath
}

func trimFilePathPrefix(path string, prefix string) (string, error) {
	// If the prefix is empty or "./", we can skip trimming as these are not actual prefixes in the file paths.
	if prefix == "" || prefix == "./" {
		return path, nil
	}

	relPath, err := filepath.Rel(prefix, path)
	if err != nil {
		return "", fmt.Errorf("failed to trim prefix from file path: %w", err)
	}
	return relPath, nil
}
