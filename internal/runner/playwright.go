package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/kballard/go-shellquote"
)

type Playwright struct {
	RunnerConfig
}

func (p Playwright) Name() string {
	return "Playwright"
}

func (p Playwright) ResultFormat() string {
	if p.ResultPath == "" {
		return ""
	}
	return "playwright-json"
}

func NewPlaywright(p RunnerConfig) Playwright {
	if p.TestCommand == "" {
		p.TestCommand = "npx playwright test"
	}

	if p.RetryTestCommand == "" {
		p.RetryTestCommand = p.TestCommand
	}

	if p.TestFilePattern == "" {
		p.TestFilePattern = "**/{*.spec,*.test}.{ts,js}"
	}

	return Playwright{
		RunnerConfig: p,
	}
}

func (p Playwright) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     true,
		SplitByExample:  true,
		FilterTestFiles: true,
		FilterTestByTag: false,
		AutoRetry:       true,
		Mute:            true,
		// Skip stays disabled for now. The same test can run across multiple
		// Playwright projects (e.g. Chromium and Firefox) as separate examples
		// that share the same Path (file:line). The value we hand to Playwright
		// is just that Path, which is not project-specific, so skipping one
		// project's example while keeping another's would still rerun both.
		// Enabling skip needs a project-scoped selector first.
		Skip:            false,
		SplitBySelector: true,
	}
}

func (p Playwright) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := BuildCommand(p, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := runAndForwardSignal(cmd)

	// Playwright exits with a non-zero status code when there are test failures,
	// so we should always attempt to parse the report even if the command returns an error.
	report, parseErr := p.parseReport(p.ResultPath)
	if parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read Playwright output, tests will not be retried: %v\n", parseErr)
		// We don't want to fail the build if we fail to parse the report,
		// therefore we return the command error (which can be nil), instead of the parse error.
		return cmdErr
	}

	for _, suite := range report.Suites {
		testResults := p.getTestResultsFromSuite(suite, suite.Title)
		for _, testResult := range testResults {
			result.RecordTestResult(testResult.TestCase, testResult.Status)
		}
	}

	if len(report.Errors) > 0 {
		result.error = fmt.Errorf("Playwright failed with errors")
	}

	// Return any command error after processing the report
	return cmdErr

}

// getTestCasesFromSuite recursively traverses the Playwright report suite and returns all test cases.
// Playwright's report format is a tree structure, where each suite can contain multiple specs and sub-suites.
// The function traverses the tree and collects failed test cases from the leaf nodes.
func (p Playwright) getTestResultsFromSuite(suite PlaywrightReportSuite, suiteName string) []TestResult {
	var testResults []TestResult

	for _, spec := range suite.Specs {
		var status TestStatus
		if !spec.Ok {
			status = TestStatusFailed
		} else if spec.Tests[0].Status == "skipped" {
			status = TestStatusSkipped
		} else {
			status = TestStatusPassed
		}

		testResults = append(testResults, TestResult{
			TestCase: mapSpecToTestCase(spec, suiteName),
			Status:   status,
		})
	}

	for _, subSuite := range suite.Suites {
		testResults = append(testResults, p.getTestResultsFromSuite(subSuite, fmt.Sprintf("%s %s", suiteName, subSuite.Title))...)
	}

	return testResults
}

func (p Playwright) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	cmd := p.TestCommand
	if retry {
		cmd = p.RetryTestCommand
	}

	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	testPaths := pathsFromTestCases(testCases)

	idx := slices.Index(words, "{{testExamples}}")
	if idx < 0 {
		words = append(words, testPaths...)
	} else {
		words = slices.Replace(words, idx, idx+1, testPaths...)
	}

	return words[0], words[1:], nil
}

// parsePlaywrightBatchReport handles the JSON report of one Playwright command
// run by bktec pool consume. A test runs once in each project and is reported
// once per project, with the project in its scope like Buildkite Test
// Collector, so muting stays per project while the attempt of the test's
// leased file accounts for every project.
func parsePlaywrightBatchReport(data []byte) (ParsedReport, error) {
	var report struct {
		Config struct {
			RootDir string `json:"rootDir"`
		} `json:"config"`
		Suites *[]PlaywrightReportSuite `json:"suites"`
		Errors []json.RawMessage        `json:"errors"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return ParsedReport{}, fmt.Errorf("invalid Playwright report: %w", err)
	}
	if report.Suites == nil || report.Config.RootDir == "" {
		return ParsedReport{}, fmt.Errorf("incomplete Playwright report: missing suites or config.rootDir")
	}
	workDir, err := os.Getwd()
	if err != nil {
		return ParsedReport{}, err
	}
	parsed := ParsedReport{
		// Playwright doesn't run any tests when a file fails to load, and reports
		// that and other errors outside tests without saying which tests they
		// affect.
		ErrorsOutsideTests: len(report.Errors) > 0,
		// Playwright runs the tests of setup projects with every command.
		UnselectedTests: true,
	}
	seen := map[string]int{}
	var walk func(suite PlaywrightReportSuite, titles string) error
	walk = func(suite PlaywrightReportSuite, titles string) error {
		for _, spec := range suite.Specs {
			if spec.File == "" {
				return fmt.Errorf("playwright test %q has no file", spec.Title)
			}
			// Spec files are relative to the root directory; selectors to the working directory.
			path, err := filepath.Rel(workDir, filepath.Join(report.Config.RootDir, spec.File))
			if err != nil {
				return fmt.Errorf("failed to get relative path of test file: %w", err)
			}
			path = filepath.ToSlash(path)
			for _, run := range spec.Tests {
				var status TestStatus
				switch run.Status {
				case "expected", "flaky":
					status = TestStatusPassed
				case "unexpected":
					status = TestStatusFailed
				case "skipped":
					status = TestStatusSkipped
					// A test that wasn't meant to be skipped didn't run because
					// something else failed, such as an earlier test in a serial
					// group. A passing retry of that test must not hide it.
					if run.ExpectedStatus != "skipped" && !slices.Contains(parsed.ErroredFiles, path) {
						parsed.ErroredFiles = append(parsed.ErroredFiles, path)
					}
				default:
					return fmt.Errorf("unknown Playwright test status %q", run.Status)
				}
				// Matches the scope of Buildkite Test Collector, as in mapSpecToTestCase.
				test := ReportedTest{
					TestCase: plan.TestCase{
						Scope: fmt.Sprintf(" %s %s %s", run.ProjectName, titles, spec.Title),
						Name:  spec.Title,
						Path:  fmt.Sprintf("%s:%d", path, spec.Line),
					},
					Status:   status,
					Selector: path,
				}
				// A test repeated in one project, such as with --repeat-each, is
				// reported once: failed if any run failed.
				key := strings.Join([]string{path, test.TestCase.Scope}, "\x00")
				if i, ok := seen[key]; ok {
					if status == TestStatusFailed || parsed.Tests[i].Status == TestStatusSkipped {
						parsed.Tests[i].Status = status
					}
					continue
				}
				seen[key] = len(parsed.Tests)
				parsed.Tests = append(parsed.Tests, test)
			}
		}
		for _, child := range suite.Suites {
			if err := walk(child, titles+" "+child.Title); err != nil {
				return err
			}
		}
		return nil
	}
	for _, suite := range *report.Suites {
		if err := walk(suite, suite.Title); err != nil {
			return ParsedReport{}, err
		}
	}
	return parsed, nil
}

func (p Playwright) parseReport(path string) (PlaywrightReport, error) {
	var report PlaywrightReport
	data, err := os.ReadFile(path)
	if err != nil {
		return PlaywrightReport{}, fmt.Errorf("failed to read playwright output: %v", err)
	}

	if err := json.Unmarshal(data, &report); err != nil {
		return PlaywrightReport{}, fmt.Errorf("failed to parse playwright output: %s", err)
	}

	return report, nil
}

func (p Playwright) DiscoverTestTargets() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", p.TestFilePattern, "exclude pattern:", p.TestFileExcludePattern)
	files, err := discoverTestFiles(p.TestFilePattern, p.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", p.TestFilePattern, p.TestFileExcludePattern)
	}

	return files, nil
}

// GetExamples returns an array of test examples within the given files.
func (p Playwright) GetExamples(files []string) ([]plan.TestCase, error) {
	if len(files) == 0 {
		return []plan.TestCase{}, nil
	}

	// Create a temporary file for JSON output
	// This approach works with both npm/npx and yarn, avoiding yarn's stdout prefix issues
	tmpFile, err := os.CreateTemp("", "playwright-list-*.json")
	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	testCases := testCasesFromPaths(files)
	cmdName, cmdArgs, err := p.CommandNameAndArgs(testCases, false)
	if err != nil {
		return []plan.TestCase{}, err
	}

	cmdArgs = append(cmdArgs, "--list", "--reporter=json")

	debug.Printf("Running `%s %s` to list tests", cmdName, strings.Join(cmdArgs, " "))

	cmd := exec.Command(cmdName, cmdArgs...)
	// Playwright's JSON reporter output path can only be set via env var, not CLI args.
	// See: https://playwright.dev/docs/test-reporters#json-reporter
	cmd.Env = append(os.Environ(), fmt.Sprintf("PLAYWRIGHT_JSON_OUTPUT_NAME=%s", tmpPath))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to list playwright tests: %w\nstderr: %s", err, stderr.String())
	}

	report, err := p.parseReport(tmpPath)
	if err != nil {
		return []plan.TestCase{}, fmt.Errorf("failed to parse playwright test list output: %w", err)
	}

	testCases = nil
	for _, suite := range report.Suites {
		testCases = append(testCases, p.getTestCasesFromSuite(suite, suite.Title)...)
	}

	return testCases, nil
}

// getTestCasesFromSuite recursively traverses the Playwright report suite and returns all test cases.
// Playwright's report format is a tree structure, where each suite can contain multiple specs and sub-suites.
func (p Playwright) getTestCasesFromSuite(suite PlaywrightReportSuite, suiteName string) []plan.TestCase {
	var testCases []plan.TestCase

	for _, spec := range suite.Specs {
		testCases = append(testCases, mapSpecToTestCase(spec, suiteName))
	}

	for _, subSuite := range suite.Suites {
		testCases = append(testCases, p.getTestCasesFromSuite(subSuite, fmt.Sprintf("%s %s", suiteName, subSuite.Title))...)
	}

	return testCases
}

// mapSpecToTestCase converts a Playwright spec to a plan.TestCase.
// The scope has to match with the scope generated by Buildkite test collector.
// In Buildkite test collector, the scope is generated using Playwright built-in reporter function, titlePath().
// titlePath function returns an array of suite's title from the root suite down to the current test,
// which is then joined with a space separator to form the scope.
// For more details, see:
// [Buildkite Test Collector - Playwright implementation](https://github.com/buildkite/test-collector-javascript/blob/42b803a618a15a07edf0169038ef4b5eba88f98d/playwright/reporter.js#L47)
// [Playwright titlePath implementation](https://github.com/microsoft/playwright/blob/523e50088a7f982dd96aacdb260dfbd1189159b1/packages/playwright/src/common/test.ts#L126)
// [Playwright suite structure](https://playwright.dev/docs/api/class-suite)
func mapSpecToTestCase(spec PlaywrightSpec, suiteName string) plan.TestCase {
	projectName := spec.Tests[0].ProjectName
	return plan.TestCase{
		Name:  spec.Title,
		Path:  fmt.Sprintf("%s:%d", spec.File, spec.Line),
		Scope: fmt.Sprintf(" %s %s %s", projectName, suiteName, spec.Title),
	}
}

type PlaywrightTest struct {
	ProjectName    string
	Status         string
	ExpectedStatus string
}

type PlaywrightSpec struct {
	File   string
	Line   int
	Column int
	ID     string
	Title  string
	Ok     bool
	Tests  []PlaywrightTest
}

type PlaywrightReportSuite struct {
	Title  string
	Specs  []PlaywrightSpec
	Suites []PlaywrightReportSuite
}

type PlaywrightReport struct {
	Suites []PlaywrightReportSuite
	Stats  struct {
		Expected   int
		Unexpected int
	}
	Errors []struct {
		Message string
	}
}
