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
func (r Rspec) GetFiles() ([]string, error) {
	pattern := r.discoveryPattern()

	files, err := discoverTestFiles(pattern)

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", pattern.IncludePattern, pattern.ExcludePattern)
	}

	return files, nil
}

// Command returns an exec.Cmd that will run the rspec command
func (Rspec) Command(testCases []string, testCommandArgs []string) *exec.Cmd {
	testCommand := "bundle exec rspec"
	testArgs := testCases

	if len(testCommandArgs) > 0 {
		index := -1
		for i, item := range testCommandArgs {
			if item == "{{testExample}}" {
				index = i
				break
			}
		}
		testCommand = strings.Join(testCommandArgs[:index], " ")
		testArgs = append(testArgs, testCommandArgs[index+1:]...)
	}

	fmt.Println(testCommand, strings.Join(testArgs, " "))

	cmd := exec.Command(testCommand, testArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}

// discoveryPattern returns the pattern to use for discovering test files.
// It uses the BUILDKITE_SPLITTER_TEST_FILE_PATTERN and BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN.
// If BUILDKITE_SPLITTER_TEST_FILE_PATTERN is not set, it defaults to "spec/**/*_spec.rb"
func (Rspec) discoveryPattern() DiscoveryPattern {
	includePattern := os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN")

	if includePattern == "" {
		includePattern = "spec/**/*_spec.rb"
	}

	excludePattern := os.Getenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN")

	return DiscoveryPattern{
		IncludePattern: includePattern,
		ExcludePattern: excludePattern,
	}
}
