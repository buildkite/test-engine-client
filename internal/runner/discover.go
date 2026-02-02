package runner

import (
	"fmt"
	"io/fs"
	"strings"

	"drjosh.dev/zzglob"
)

// splitPatterns splits a comma-separated pattern string while respecting
// brace groupings. For example, "**/*.{js,ts},spec/**" returns
// ["**/*.{js,ts}", "spec/**"] rather than splitting inside the braces.
func splitPatterns(pattern string) []string {
	var patterns []string
	var current strings.Builder
	braceDepth := 0

	for _, ch := range pattern {
		switch ch {
		case '{':
			braceDepth++
			current.WriteRune(ch)
		case '}':
			braceDepth--
			current.WriteRune(ch)
		case ',':
			if braceDepth > 0 {
				current.WriteRune(ch)
			} else {
				patterns = append(patterns, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		patterns = append(patterns, current.String())
	}

	return patterns
}

func discoverTestFiles(pattern string, excludePattern string) ([]string, error) {
	patterns := splitPatterns(pattern)

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
