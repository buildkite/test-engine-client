package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

type GoTest struct {
	RunnerConfig
}

type testTiming struct {
	start time.Time
	end   time.Time
}

func (g GoTest) Name() string {
	return "Go"
}

func NewGoTest(g RunnerConfig) GoTest {
	if g.TestCommand == "" {
		g.TestCommand = "go test {{testExamples}} -json"
	}

	if g.TestFilePattern == "" {
		g.TestFilePattern = "**/*_test.go"
	}

	if g.RetryTestCommand == "" {
		g.RetryTestCommand = "go test {{testExamples}} -run '{{testNamePattern}}' -json"
	}

	return GoTest{
		RunnerConfig: g,
	}
}

func (g GoTest) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	// Group test cases by package
	pkgTests := make(map[string][]plan.TestCase)
	for _, tc := range testCases {
		pkg := filepath.Dir(tc.Path)
		pkgTests[pkg] = append(pkgTests[pkg], tc)
	}

	// Run tests package by package
	for pkg, tests := range pkgTests {
		// Build test pattern for this package
		var testPattern string
		var command string
		if retry {
			testPattern = buildTestNamePattern(tests)
			command = g.RetryTestCommand
		} else {
			// Build pattern to match only tests from specified files
			var patterns []string
			for _, tc := range tests {
				// If test name is specified, use it
				if tc.Name != "" {
					patterns = append(patterns, "^"+tc.Name+"$")
				} else {
					// Otherwise, get all tests from the file
					fileTests, err := g.getTestsInFile(tc.Path)
					if err != nil {
						return fmt.Errorf("failed to get tests in file %s: %w", tc.Path, err)
					}
					for _, test := range fileTests {
						patterns = append(patterns, "^"+test+"$")
					}
				}
			}
			if len(patterns) > 0 {
				testPattern = strings.Join(patterns, "|")
				command = fmt.Sprintf("go test {{testExamples}} -run '%s' -json", testPattern)
			} else {
				command = g.TestCommand
			}
		}

		// Build command for this package
		cmdName, cmdArgs, err := g.commandNameAndArgs(command, []string{"./" + pkg}, testPattern)
		if err != nil {
			result.err = err
			return fmt.Errorf("failed to build command: %w", err)
		}

		debug.Printf("Running command: %s %v", cmdName, cmdArgs)
		cmd := exec.Command(cmdName, cmdArgs...)

		// Set up pipe for stdout to capture JSON output
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			result.err = err
			return fmt.Errorf("failed to create stdout pipe: %w", err)
		}

		// Set stderr to os.Stderr for immediate error output
		cmd.Stderr = os.Stderr

		// Start the command
		if err := cmd.Start(); err != nil {
			result.err = err
			return fmt.Errorf("failed to start command: %w", err)
		}

		// Create a map of package paths to test files
		filePackages := make(map[string]string)
		for _, tc := range tests {
			dir := filepath.Dir(tc.Path)
			pkg := filepath.Base(dir)
			filePackages[pkg] = tc.Path
		}

		// Parse JSON output and record test results
		scanner := bufio.NewScanner(stdout)
		var testFailed bool

		// Track test timings
		testTimings := make(map[string]*testTiming)

		testOutput := make(map[string][]string)

		for scanner.Scan() {
			var event struct {
				Action  string  `json:"Action"`
				Test    string  `json:"Test"`
				Output  string  `json:"Output"`
				Package string  `json:"Package"`
				Elapsed float64 `json:"Elapsed"`
				Time    string  `json:"Time"`
			}

			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue // Skip malformed JSON
			}

			// Get the file path for this test's package
			pkg := filepath.Base(event.Package)
			testFile := filePackages[pkg]

			// Capture test output
			if event.Test != "" && event.Output != "" {
				testOutput[event.Test] = append(testOutput[event.Test], event.Output)
			}

			// Record test results
			switch event.Action {
			case "run":
				if event.Test != "" {
					testTimings[event.Test] = &testTiming{
						start: time.Now(),
					}
				}
			case "fail", "pass":
				if event.Test != "" {
					// Record timing
					if timing := testTimings[event.Test]; timing != nil {
						timing.end = time.Now()
						_ = timing.end.Sub(timing.start) // Ignore duration until RunResult supports it
					}

					// Record test result
					status := TestStatusPassed
					if event.Action == "fail" {
						status = TestStatusFailed
						testFailed = true
					}

					result.RecordTestResult(plan.TestCase{
						Name: event.Test,
						Path: testFile,
					}, status)

					// TODO: Record test output when RunResult supports it
					// output := strings.Join(testOutput[event.Test], "")
				}
			}

			// Forward the output
			fmt.Println(scanner.Text())
		}

		// After scanner loop
		if err := scanner.Err(); err != nil {
			result.err = fmt.Errorf("error reading test output: %w", err)
			return result.err
		}

		// Wait for the command to complete
		err = cmd.Wait()
		if err != nil || testFailed {
			if testFailed {
				// Don't set result.err for test failures, just return an error
				return fmt.Errorf("tests failed")
			}
			// For other errors, set both result.err and return the error
			result.err = err
			return err
		}
	}

	return nil
}

func (g GoTest) GetFiles() ([]string, error) {
	files, err := discoverTestFiles(g.TestFilePattern, g.TestFileExcludePattern)
	if err != nil {
		return nil, err
	}
	// Sort files to ensure consistent order
	sort.Strings(files)
	return files, nil
}

func (g GoTest) GetExamples(files []string) ([]plan.TestCase, error) {
	// Group files by package
	pkgFiles := make(map[string][]string)
	for _, file := range files {
		pkg := filepath.Dir(file)
		pkgFiles[pkg] = append(pkgFiles[pkg], file)
	}

	var testCases []plan.TestCase
	for pkg, pkgFiles := range pkgFiles {
		// Run test listing once per package
		args := []string{"test", "-list", "^Test"}
		args = append(args, pkgFiles...)

		cmd := exec.Command("go", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("failed to list tests in package %s: %w", pkg, err)
		}

		// Parse output and create test cases
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		for scanner.Scan() {
			testName := scanner.Text()
			if strings.HasPrefix(testName, "Test") {
				// For each test, find which file it belongs to
				for _, file := range pkgFiles {
					// Check if the test exists in this file
					if containsTest(file, testName) {
						testCases = append(testCases, plan.TestCase{
							Name: testName,
							Path: file,
						})
						break
					}
				}
			}
		}
	}

	// Sort test cases by path and name
	sort.Slice(testCases, func(i, j int) bool {
		if testCases[i].Path != testCases[j].Path {
			return testCases[i].Path < testCases[j].Path
		}
		return testCases[i].Name < testCases[j].Name
	})

	return testCases, nil
}

// containsTest checks if a test function exists in a file
func containsTest(file, testName string) bool {
	content, err := os.ReadFile(file)
	if err != nil {
		return false
	}

	// Look for the function definition
	pattern := fmt.Sprintf("func %s", testName)
	return strings.Contains(string(content), pattern)
}

// Helper function to build test name pattern for retries
func buildTestNamePattern(testCases []plan.TestCase) string {
	patterns := make([]string, len(testCases))
	for i, tc := range testCases {
		patterns[i] = fmt.Sprintf("%s/%s", tc.Scope, tc.Name)
	}
	return strings.Join(patterns, "|")
}

func (g GoTest) commandNameAndArgs(command string, testExamples []string, testNamePattern string) (string, []string, error) {
	parts, err := shellquote.Split(command)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse command: %w", err)
	}

	for i, part := range parts {
		part = strings.ReplaceAll(part, "{{testExamples}}", strings.Join(testExamples, " "))
		if testNamePattern != "" {
			part = strings.ReplaceAll(part, "{{testNamePattern}}", testNamePattern)
		}
		parts[i] = part
	}

	if len(parts) == 0 {
		return "", nil, fmt.Errorf("no command provided")
	}

	return parts[0], parts[1:], nil
}

// getTestsInFile returns all test names in a given file
func (g GoTest) getTestsInFile(file string) ([]string, error) {
	cmd := exec.Command("go", "test", "-list", "^Test", file)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list tests in %s: %w", file, err)
	}

	var tests []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		testName := scanner.Text()
		if strings.HasPrefix(testName, "Test") {
			tests = append(tests, testName)
		}
	}
	return tests, nil
}
