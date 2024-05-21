package runner

import (
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/kballard/go-shellquote"
)

// In future, Rspec will implement an interface that defines
// behaviour common to all test runners.
// For now, Rspec provides rspec specific behaviour to execute
// and report on tests in the Rspec framework.
type Rspec struct {
	TestCommand string
}

func NewRspec(testCommand string) Rspec {
	if testCommand == "" {
		testCommand = "bundle exec rspec {{testExamples}}"
	}

	return Rspec{
		TestCommand: testCommand,
	}
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

func (r Rspec) RetryCommand(defaultTestCommand string) (*exec.Cmd, error) {
	// use default test command to build retry command
	// remove all occurrences of "{{testExamples}}" and append "--only-failures"

	// TODO: support custom retry command in the future
	words, err := shellquote.Split(defaultTestCommand)
	if err != nil {
		return nil, err
	}
	words = slices.DeleteFunc(words, func(n string) bool {
		return n == "{{testExamples}}"
	})
	words = slices.Insert(words, len(words), "--only-failures")
	fmt.Println(shellquote.Join(words...))

	cmd := exec.Command(words[0], words[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
}

// Command returns an exec.Cmd that will run the rspec command
func (r Rspec) Command(testCases []string) (*exec.Cmd, error) {
	commandName, commandArgs, err := r.commandNameAndArgs(testCases)
	if err != nil {
		return nil, err
	}
	fmt.Println(shellquote.Join(append([]string{commandName}, commandArgs...)...))

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
func (r Rspec) commandNameAndArgs(testCases []string) (string, []string, error) {
	words, err := shellquote.Split(r.TestCommand)
	if err != nil {
		return "", []string{}, err
	}
	idx := slices.Index(words, "{{testExamples}}")
	if idx < 0 {
		words = append(words, testCases...)
		return words[0], words[1:], nil
	}
	words = slices.Replace(words, idx, idx+1, testCases...)
	return words[0], words[1:], nil
}
