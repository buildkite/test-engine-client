package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// In future, Rspec will implement an interface that defines
// behaviour common to all test runners.
// For now, Rspec provides rspec specific behaviour to execute
// and report on tests in the Rspec framework.
type Rspec struct {
}

// GetFiles returns an array of file names, for files in
// the "spec" directory that end in "spec.rb".
func (Rspec) GetFiles() ([]string, error) {
	pattern := os.Getenv("BUILDKITE_SPLITTER_PATTERN")

	// set default Rspec pattern if not provided
	if pattern == "" {
		pattern = "spec/**/*_spec.rb"
	}

	excludePattern := os.Getenv("BUILDKITE_SPLITTER_EXCLUDE_PATTERN")

	files, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q", pattern)
	}

	return files, nil
}

// Command returns an exec.Cmd that will run the rspec command
func (Rspec) Command(testCases []string) *exec.Cmd {
	args := []string{"--options", ".rspec.ci"}

	args = append(args, testCases...)

	fmt.Println("bin/rspec", strings.Join(args, " "))

	cmd := exec.Command("bin/rspec", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}
