package runner

import (
	"fmt"
	"io/fs"
	"strings"

	"drjosh.dev/zzglob"
)

func discoverTestFiles(pattern string, excludePattern string) ([]string, error) {
	patterns := strings.Split(pattern, ",")

	parsedExcludePattern, err := zzglob.Parse(excludePattern)
	if err != nil {
		return nil, fmt.Errorf("error parsing test file exclude pattern %q", excludePattern)
	}

	var discoveredFiles []string
	seen := make(map[string]bool)

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		parsedPattern, err := zzglob.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("error parsing test file pattern %q", p)
		}

		err = parsedPattern.Glob(func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Printf("Error walking at path %q: %v\n", path, err)
				return nil
			}

			if parsedExcludePattern.Match(path) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			if d.Name() == "node_modules" {
				return fs.SkipDir
			}

			if d.IsDir() {
				return nil
			}

			if !seen[path] {
				seen[path] = true
				discoveredFiles = append(discoveredFiles, path)
			}
			return nil
		}, zzglob.WalkIntermediateDirs(true))

		if err != nil {
			return nil, fmt.Errorf("error walking directory: %v", err)
		}
	}

	return discoveredFiles, nil
}
