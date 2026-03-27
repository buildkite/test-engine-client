package runner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

type NUnit struct {
	RunnerConfig
}

// Compile-time check that NUnit implements TestRunner
var _ TestRunner = (*NUnit)(nil)

func NewNUnit(c RunnerConfig) NUnit {
	if c.TestCommand == "" {
		c.TestCommand = "dotnet test --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/*Tests.cs"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	return NUnit{
		RunnerConfig: c,
	}
}

func (n NUnit) Name() string {
	return "NUnit"
}

// GetFiles returns an array of .cs test file names using the discovery pattern.
func (n NUnit) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", n.TestFilePattern, "exclude pattern:", n.TestFileExcludePattern)
	files, err := discoverTestFiles(n.TestFilePattern, n.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", n.TestFilePattern, n.TestFileExcludePattern)
	}

	return files, nil
}

func (n NUnit) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in NUnit")
}

// Run executes dotnet test with a --filter expression built from the test cases.
// Test cases are mapped from .cs file paths to class names, and joined into a
// FullyQualifiedName~ filter expression.
func (n NUnit) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	command := n.TestCommand
	if retry {
		command = n.RetryTestCommand
	}

	classNames := extractClassNames(testCases)

	cmdName, cmdArgs, err := n.commandNameAndArgs(command, classNames)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	cmdErr := runAndForwardSignal(cmd)

	testResults, parseErr := loadAndParseJUnitXmlResult(n.ResultPath)
	if parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read NUnit output, tests will not be retried: %v\n", parseErr)
		return cmdErr
	}

	for _, test := range testResults {
		result.RecordTestResult(plan.TestCase{
			Scope: test.Classname,
			Name:  test.Name,
			Path:  test.Classname,
		}, test.Result)
	}

	return cmdErr
}

// extractClassNames extracts unique class names from test case paths.
// Each path is expected to be a .cs file path like "tests/MyLib.Tests/CalculatorTests.cs".
// The class name is the filename without extension, e.g. "CalculatorTests".
func extractClassNames(testCases []plan.TestCase) []string {
	seen := map[string]bool{}
	var classNames []string

	for _, tc := range testCases {
		className := strings.TrimSuffix(filepath.Base(tc.Path), ".cs")
		if !seen[className] {
			classNames = append(classNames, className)
			seen[className] = true
		}
	}

	return classNames
}

// buildTestFilter constructs a dotnet test --filter expression from class names.
// Each class name becomes a "FullyQualifiedName~.ClassName" predicate,
// joined with "|" (OR).
// The "~" operator is a "contains" match and the leading "." anchors to a
// namespace boundary, preventing false positives on partial name matches.
func buildTestFilter(classNames []string) string {
	parts := make([]string, len(classNames))
	for i, name := range classNames {
		parts[i] = fmt.Sprintf("FullyQualifiedName~.%s", name)
	}
	return strings.Join(parts, "|")
}

func (n NUnit) commandNameAndArgs(cmd string, classNames []string) (string, []string, error) {
	filter := buildTestFilter(classNames)

	cmd = strings.Replace(cmd, "{{testFilter}}", filter, 1)
	cmd = strings.Replace(cmd, "{{resultPath}}", n.ResultPath, 1)

	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	return words[0], words[1:], nil
}
