package runner

import (
	"fmt"
	"os"
	"os/exec"
	"slices"

	"github.com/buildkite/test-splitter/internal/debug"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/kballard/go-shellquote"
)

type Jest struct {
	TestCommand            string
	TestFileExcludePattern string
	TestFilePattern        string
	RetryTestCommand       string
}

func NewJest(j Jest) *Jest {
	if j.TestCommand == "" {
		j.TestCommand = "yarn test -- {{testExamples}}"
	}

	if j.TestFilePattern == "" {
		j.TestFilePattern = "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}"
	}

	return &j
}

// GetFiles returns an array of file names using the discovery pattern.
func (j Jest) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", j.TestFilePattern, "exclude pattern:", j.TestFileExcludePattern)
	files, err := discoverTestFiles(j.TestFilePattern, j.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", j.TestFilePattern, j.TestFileExcludePattern)
	}

	return files, nil
}

func (j Jest) Command(testCases []string) (*exec.Cmd, error) {
	commandName, commandArgs, err := j.commandNameAndArgs(testCases)
	if err != nil {
		return nil, err
	}
	fmt.Println(shellquote.Join(append([]string{commandName}, commandArgs...)...))

	cmd := exec.Command(commandName, commandArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
}

func (j Jest) RetryCommand() (*exec.Cmd, error) {
	cmd := exec.Command(j.TestCommand)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd, nil
}

func (j Jest) commandNameAndArgs(testCases []string) (string, []string, error) {
	words, err := shellquote.Split(j.TestCommand)
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

func (j Jest) GetExamples(files []string) ([]plan.TestCase, error) {
	return []plan.TestCase{}, nil
}
