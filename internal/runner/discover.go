package runner

import (
	"fmt"
	"io/fs"

	"drjosh.dev/zzglob"
)

func discoverTestFiles(pattern string, excludePattern string) ([]string, error) {
	parsedPattern, err := zzglob.Parse(pattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing test file pattern %q", pattern)
	}

	parsedExcludePattern, err := zzglob.Parse(excludePattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing test file exclude pattern %q", excludePattern)
	}

	discoveredFiles := []string{}

	// Use the Glob function to traverse the directory recursively
	// and append the matched file paths to the discoveredFiles slice
	err = parsedPattern.Glob(func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("Error walking at path %q: %v\n", path, err)
			return nil
		}

		// Check if the path matches the exclude pattern. If so, skip it.
		// If it matches a directory, then skip that directory.
		if parsedExcludePattern.Match(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Skip the node_modules directory
		if d.Name() == "node_modules" {
			return fs.SkipDir
		}

		// Skip directories that happen to match the include pattern - we're
		// only interested in files.
		if d.IsDir() {
			return nil
		}

		discoveredFiles = append(discoveredFiles, path)
		return nil
	}, zzglob.WalkIntermediateDirs(true))

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %v", err)
	}

	return discoveredFiles, nil
}
