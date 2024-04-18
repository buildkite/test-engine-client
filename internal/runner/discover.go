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
		return nil, fmt.Errorf("error parsing pattern %q", pattern)
	}

	parsedExcludePattern, err := zzglob.Parse(pattern.ExcludePattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing exclude pattern %q", pattern.ExcludePattern)
	}

	discoveredFiles := []string{}

	// Use the Glob function to traverse the directory recursively
	// and append the matched file paths to the discoveredFiles slice
	parsedPattern.Glob(func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("Error walking: %v\n", err)
		}
		if d.IsDir() {
			return nil
		}

		// Check if the path matches the exclude pattern and skip it
		if parsedExcludePattern.Match(path) {
			return nil
		}

		discoveredFiles = append(discoveredFiles, path)
		return nil
	}, nil)

	return discoveredFiles, nil
}
