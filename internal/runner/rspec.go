package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kballard/go-shellquote"
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
func (r Rspec) Command(testCases []string, testCommand string) (*exec.Cmd, error) {
	commandName, commandArgs, err := r.commandNameAndArgs(testCases, testCommand)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%q\n", commandName+" "+strings.Join(commandArgs, " "))

	cmd := exec.Command(commandName, commandArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
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

// commandNameAndArgs returns the command name and arguments to run the Rspec tests
func (Rspec) commandNameAndArgs(testCases []string, testCommand string) (string, []string, error) {
	completedTestCommand := ""
	if strings.Contains(testCommand, "{{testExamples}}") {
		completedTestCommand = strings.ReplaceAll(testCommand, "{{testExamples}}", shellquote.Join(testCases...))
	} else {
		completedTestCommand = testCommand + " " + shellquote.Join(testCases...)
	}

	testCommandFields, err := shellquote.Split(completedTestCommand)

	if err != nil {
		return "", []string{}, err
	}

	return testCommandFields[0], testCommandFields[1:], nil
}
