package runner

import (
	"fmt"
	"io/fs"

	"github.com/DrJosh9000/zzglob"
)

type DiscoveryPattern struct {
	IncludePattern string
	ExcludePattern string
}

func discoverTestFiles(pattern DiscoveryPattern) ([]string, error) {
	parsedPattern, err := zzglob.Parse(pattern.IncludePattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing test file pattern %q", pattern)
	}

	parsedExcludePattern, err := zzglob.Parse(pattern.ExcludePattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing test file exclude pattern %q", pattern.ExcludePattern)
	}

	discoveredFiles := []string{}

	// Use the Glob function to traverse the directory recursively
	// and append the matched file paths to the discoveredFiles slice
	parsedPattern.Glob(func(path string, d fs.DirEntry, err error) error {
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
		// Skip directories that happen to match the include pattern - we're
		// only interested in files.
		if d.IsDir() {
			return nil
		}

		discoveredFiles = append(discoveredFiles, path)
		return nil
	}, zzglob.WalkIntermediateDirs(true))

	return discoveredFiles, nil
}
