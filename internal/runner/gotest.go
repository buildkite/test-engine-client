package runner

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/kballard/go-shellquote"
)

type GoTest struct {
	RunnerConfig
	resultFormat string
}

const (
	goTestResultFormatJUnit   = "junit"
	goTestResultFormatGoJSONL = "go-jsonl"
)

// Compile-time check that GoTest implements TestRunner
var _ TestRunner = (*GoTest)(nil)

func NewGoTest(c RunnerConfig) GoTest {
	if c.TestCommand == "" {
		c.TestCommand = "gotestsum --jsonfile={{resultPath}} {{packages}}"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	resultFormat := goTestResultFormatJUnit
	if commandProducesGoJSONL(c.TestCommand) {
		resultFormat = goTestResultFormatGoJSONL
	}

	return GoTest{
		RunnerConfig: c,
		resultFormat: resultFormat,
	}
}

func (g GoTest) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     false,
		SplitByExample:  true,
		SplitBySelector: true,
		FilterTestFiles: false,
		FilterTestByTag: false,
		AutoRetry:       true,
		Mute:            true,
		Skip:            false,
	}
}

func (g GoTest) Name() string {
	return "gotest"
}

func (g GoTest) ResultFormat() string {
	return g.resultFormat
}

func commandProducesGoJSONL(command string) bool {
	args, err := shellquote.Split(command)
	if err != nil {
		return strings.Contains(command, "--jsonfile") || strings.Contains(command, "-json")
	}
	return isGoTestJSONCommand(args) || goJSONLFileArg(args) != ""
}

// Run executes the configured command for the specified packages.
func (g GoTest) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	// Preserve package retries (including custom wrapper commands) unless the
	// user opted into individual-test splitting.
	if retry && !g.SplitByExample {
		packages, err := g.getPackages(testCases)
		if err != nil {
			return err
		}
		testCases = make([]plan.TestCase, 0, len(packages))
		for _, pkg := range packages {
			testCases = append(testCases, plan.TestCase{Format: plan.TestCaseFormatSelector, Value: pkg})
		}
	}
	if slices.ContainsFunc(testCases, func(tc plan.TestCase) bool { return tc.Format == plan.TestCaseFormatExample }) {
		return g.runExamples(result, testCases, retry)
	}
	return g.run(result, testCases, retry)
}

// A Go -run filter applies to every package in a command. Run each package
// separately so identically named tests in other packages cannot leak into the
// assignment, then combine their raw reports for the normal upload path.
func (g GoTest) runExamples(result *RunResult, testCases []plan.TestCase, retry bool) error {
	packages, err := g.getPackages(testCases)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "bktec-go-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	var report strings.Builder
	if g.resultFormat == goTestResultFormatJUnit {
		report.WriteString("<testsuites>")
	}
	var runErr error
	for i, pkg := range packages {
		var cases []plan.TestCase
		for _, tc := range testCases {
			if packageFromTestCase(tc) == pkg {
				cases = append(cases, tc)
			}
		}
		// A whole-package assignment already includes all its examples.
		for _, tc := range cases {
			if tc.Format != plan.TestCaseFormatExample {
				cases = []plan.TestCase{tc}
				break
			}
		}
		part := g
		part.ResultPath = filepath.Join(dir, fmt.Sprintf("%d", i))
		cmd, err := BuildCommand(part, cases, retry)
		if err != nil {
			result.error = err
			runErr = errors.Join(runErr, err)
			break
		}
		err = part.runCommand(cmd)
		runErr = errors.Join(runErr, err)
		subset := NewRunResult(nil)
		if parseErr := part.parseResults(subset); parseErr != nil {
			result.error = fmt.Errorf("parsing Go subset report for %s: %w", pkg, parseErr)
			runErr = errors.Join(runErr, result.error)
			break
		}
		for _, test := range subset.tests {
			result.RecordTestResult(test.TestCase, test.Status)
		}
		if subset.error != nil {
			result.error = subset.error
		}
		if err == nil {
			for _, tc := range cases {
				if tc.Format != plan.TestCaseFormatExample {
					continue
				}
				tc.Name, _, _ = strings.Cut(tc.Name, "/")
				tc.Scope = pkg
				if _, ok := subset.tests[testIdentifier(tc)]; !ok {
					result.error = fmt.Errorf("Go package %s did not report assigned test %s", pkg, tc.Name)
					runErr = errors.Join(runErr, result.error)
				}
			}
		}
		data, readErr := os.ReadFile(part.ResultPath)
		if readErr == nil && g.resultFormat == goTestResultFormatJUnit {
			var suites struct {
				XMLName xml.Name `xml:"testsuites"`
				Content string   `xml:",innerxml"`
			}
			readErr = xml.Unmarshal(data, &suites)
			data = []byte(suites.Content)
		}
		if readErr != nil {
			result.error = fmt.Errorf("reading Go subset report for %s: %w", pkg, readErr)
			runErr = errors.Join(runErr, result.error)
			break
		}
		report.Write(data)
		report.WriteByte('\n')
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				result.error = err
				break
			}
		}
	}
	if g.resultFormat == goTestResultFormatJUnit {
		report.WriteString("</testsuites>")
	}
	if err := os.WriteFile(g.ResultPath, []byte(report.String()), 0600); err != nil {
		result.error = fmt.Errorf("writing combined Go report: %w", err)
		return errors.Join(runErr, result.error)
	}
	return runErr
}

func (g GoTest) run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := BuildCommand(g, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := g.runCommand(cmd)

	// go test output does not differentiate build fail or test fail. They both return 1
	// What is even more bizarre is that even when go test failed on compilation, it will still generate an output xml
	// file that says "TestMain" failed..
	if exitError := new(exec.ExitError); errors.As(cmdErr, &exitError) && exitError.ExitCode() != 1 {
		return cmdErr
	}

	if parseErr := g.parseResults(result); parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read Go test output, tests will not be retried: %v\n", parseErr)
		// We don't want to fail the build if we fail to parse the report,
		// therefore we return the command error (which can be nil), instead of the parse error.
		return cmdErr
	}

	// Return any command error after processing the report
	return cmdErr
}

func (g GoTest) runCommand(cmd *exec.Cmd) error {
	if !g.capturesGoJSONLStdout(cmd.Args) {
		return runAndForwardSignal(cmd)
	}

	file, err := os.Create(g.ResultPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return runAndForwardSignalWithOutput(cmd, io.MultiWriter(os.Stdout, file), os.Stderr)
}

func (g GoTest) capturesGoJSONLStdout(args []string) bool {
	return g.resultFormat == goTestResultFormatGoJSONL && isGoTestJSONCommand(args) && goJSONLFileArg(args) == ""
}

func (g GoTest) parseResults(result *RunResult) error {
	switch g.resultFormat {
	case goTestResultFormatJUnit:
		return g.parseJUnitResults(result)
	case goTestResultFormatGoJSONL:
		return g.parseGoJSONLResults(result)
	default:
		return fmt.Errorf("unsupported Go test result format %q", g.resultFormat)
	}
}

func (g GoTest) parseJUnitResults(result *RunResult) error {
	testResults, parseErr := loadAndParseJUnitXML(g.ResultPath)
	if parseErr != nil {
		return parseErr
	}

	for _, test := range testResults {
		// A package that fails to build is reported by gotestsum as a synthetic
		// testcase named "TestMain" with an empty classname, whose failure
		// message contains "[build failed]" (printed by the go tool itself).
		// A build failure cannot be fixed by retrying, so treat it as an error
		// outside of the tests instead of recording it as a test result.
		// Recording it would select it for retry, where its empty package path
		// would silently drop it from the retry command, allowing the build
		// failure to go unnoticed.
		if isBuildFailure(test) {
			result.error = fmt.Errorf("go test failed to build %s", test.SuiteName)
			continue
		}

		result.RecordTestResult(plan.TestCase{
			Format: plan.TestCaseFormatExample,
			Scope:  test.Classname,
			Name:   test.Name,
			// This is the special thing about go test support.
			Path: test.Classname,
		}, test.Result)
	}

	return nil
}

type goJSONLTestEvent struct {
	Action      string `json:"Action"`
	Package     string `json:"Package"`
	Test        string `json:"Test"`
	Output      string `json:"Output"`
	FailedBuild string `json:"FailedBuild"`
}

func (g GoTest) parseGoJSONLResults(result *RunResult) error {
	events, err := loadGoJSONLTestEvents(g.ResultPath)
	if err != nil {
		return err
	}

	packageOutputs := map[string]string{}
	packageFailed := map[string]bool{}
	packageBuildFailed := map[string]bool{}
	packageHasFailedTest := map[string]bool{}
	testStatuses := map[string]TestStatus{}
	testCases := map[string]plan.TestCase{}

	for _, event := range events {
		if event.Package == "" {
			continue
		}
		if event.Output != "" {
			packageOutputs[event.Package] += event.Output
		}
		if event.Test == "" {
			if event.Action == "fail" {
				packageFailed[event.Package] = true
				if event.FailedBuild != "" {
					packageBuildFailed[event.Package] = true
				}
			}
			continue
		}

		status, ok := goJSONLActionStatus(event.Action)
		if !ok {
			continue
		}

		key := event.Package + "/" + event.Test
		if testStatuses[key] != TestStatusFailed {
			testStatuses[key] = status
		}
		if status == TestStatusFailed {
			packageHasFailedTest[event.Package] = true
		}
		testCases[key] = plan.TestCase{
			Format: plan.TestCaseFormatExample,
			Scope:  event.Package,
			Name:   event.Test,
			Path:   event.Package,
		}
	}

	for key, testCase := range testCases {
		result.RecordTestResult(testCase, testStatuses[key])
	}

	for pkg := range packageFailed {
		if packageBuildFailed[pkg] || strings.Contains(packageOutputs[pkg], "[build failed]") {
			result.error = fmt.Errorf("go test failed to build %s", pkg)
			continue
		}

		if !packageHasFailedTest[pkg] {
			result.RecordTestResult(plan.TestCase{
				Format: plan.TestCaseFormatSelector,
				Value:  pkg,
				Scope:  pkg,
				Name:   "TestMain",
				Path:   pkg,
			}, TestStatusFailed)
		}
	}

	return nil
}

func loadGoJSONLTestEvents(path string) ([]goJSONLTestEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read go jsonl: %w", err)
	}
	defer file.Close()

	var events []goJSONLTestEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event goJSONLTestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("failed to parse go jsonl: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read go jsonl: %w", err)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("failed to parse go jsonl: no events found")
	}

	return events, nil
}

func goJSONLActionStatus(action string) (TestStatus, bool) {
	switch action {
	case "pass":
		return TestStatusPassed, true
	case "fail":
		return TestStatusFailed, true
	case "skip":
		return TestStatusSkipped, true
	default:
		return TestStatusUnknown, false
	}
}

// isBuildFailure reports whether a JUnit testcase is gotestsum's synthetic
// representation of a Go package that failed to build. The "TestMain" name and
// empty classname signature is also produced by package-level failures such as
// a crashing TestMain or a test timeout, so the failure content is checked for
// the "[build failed]" marker to identify build failures specifically.
func isBuildFailure(test JUnitXMLTestCase) bool {
	return test.Name == "TestMain" &&
		test.Classname == "" &&
		test.Failure != nil &&
		strings.Contains(test.Failure.Content, "[build failed]")
}

// DiscoverTestTargets discovers the Go packages that contain tests and returns
// their import paths to be used as discovery targets.
//
// "File" does not exist as a first level concept in Go projects, so package
// import paths are sent to the test plan API as selector values. Slow packages
// can subsequently be expanded into top-level tests by GetExamples.
//
// We use a format template so `go list` only emits packages that actually have
// test files (_test.go). Packages with zero tests would otherwise be sent to the
// test plan API as split units that run no tests, taking up a bin packing slot
// for nothing.
func (g GoTest) DiscoverTestTargets() ([]string, error) {
	debug.Println("Discovering Go packages with tests using `go list`")
	cmd := exec.Command("go", "list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./...")
	output, err := cmd.Output()
	if err != nil {
		// Handle stderr for better error messages
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list failed: %w\nstderr:\n%s", err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}
	packages := strings.Split(strings.TrimSpace(string(output)), "\n")
	// Packages without tests produce an empty line in the output, so filter them out.
	validPackages := []string{}
	for _, pkg := range packages {
		if pkg != "" {
			validPackages = append(validPackages, pkg)
		}
	}
	debug.Println("Discovered", len(validPackages), "packages with tests")
	if len(validPackages) == 0 {
		return nil, fmt.Errorf("no Go packages with tests found using `go list`")
	}
	return validPackages, nil
}

// GetExamples lists top-level tests, examples and fuzz seed tests without running
// their bodies. Go cannot list dynamically generated subtests: they remain part
// of their top-level parent. Use the configured command so build flags and
// environment are identical to execution. Package init and TestMain still run.
func (g GoTest) GetExamples(packages []string) ([]plan.TestCase, error) {
	dir, err := os.MkdirTemp("", "bktec-go-list-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	g.ResultPath = filepath.Join(dir, "report")
	var examples []plan.TestCase
	for _, pkg := range packages {
		cmd, err := BuildCommand(g, []plan.TestCase{{Format: plan.TestCaseFormatSelector, Value: pkg}}, false)
		if err != nil {
			return nil, err
		}
		jsonPath := filepath.Join(dir, "events.jsonl")
		switch filepath.Base(cmd.Args[0]) {
		case "go":
			if len(cmd.Args) < 2 || cmd.Args[1] != "test" {
				return nil, fmt.Errorf("Go example discovery requires go test or gotestsum")
			}
			cmd.Args = append(cmd.Args, "-json")
		case "gotestsum":
			index := slices.Index(cmd.Args, "--")
			if index < 0 {
				index = slices.Index(cmd.Args, pkg)
				cmd.Args = slices.Insert(cmd.Args, index, "--")
			}
			cmd.Args = slices.Insert(cmd.Args, index, "--jsonfile="+jsonPath)
		default:
			return nil, fmt.Errorf("Go example discovery requires go test or gotestsum")
		}
		cmd.Args = append(cmd.Args, "-list=.")
		output, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				output = append(output, exitErr.Stderr...)
			}
			return nil, fmt.Errorf("listing tests in %s: %w\n%s", pkg, err, output)
		}
		if filepath.Base(cmd.Args[0]) == "gotestsum" {
			output, err = os.ReadFile(jsonPath)
			if err != nil {
				return nil, fmt.Errorf("reading Go discovery events: %w", err)
			}
		}
		for _, line := range strings.Split(string(output), "\n") {
			var event goJSONLTestEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil || event.Action != "output" {
				continue
			}
			name := strings.TrimSuffix(event.Output, "\n")
			if strings.ContainsAny(name, " \t\r\n/") || !(strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")) {
				continue
			}
			examples = append(examples, plan.TestCase{
				Format: plan.TestCaseFormatExample,
				Name:   name, Scope: pkg, Path: pkg,
			})
		}
	}
	return examples, nil
}

func (g GoTest) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	packages, err := g.getPackages(testCases)
	if err != nil {
		return "", []string{}, fmt.Errorf("failed to generate test package list: %w", err)
	}

	cmd := g.TestCommand
	if retry {
		cmd = g.RetryTestCommand
	}

	concatenatedPackages := strings.Join(packages, " ")

	if strings.Contains(cmd, "{{packages}}") {
		cmd = strings.Replace(cmd, "{{packages}}", concatenatedPackages, 1)
	} else {
		cmd = cmd + " " + concatenatedPackages
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", g.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	var names []string
	for _, tc := range testCases {
		if tc.Format != plan.TestCaseFormatExample {
			continue
		}
		if len(packages) != 1 {
			return "", nil, fmt.Errorf("Go example subsets must run one package at a time")
		}
		name, _, _ := strings.Cut(tc.Name, "/")
		if name == "" {
			return "", nil, fmt.Errorf("Go example has no test name")
		}
		names = append(names, regexp.QuoteMeta(name))
	}
	if len(names) > 0 {
		for _, arg := range args {
			if arg == "-run" || strings.HasPrefix(arg, "-run=") || arg == "-test.run" || strings.HasPrefix(arg, "-test.run=") {
				return "", nil, fmt.Errorf("Go example splitting controls -run; remove the configured -run filter")
			}
			flag, _, _ := strings.Cut(arg, "=")
			if flag == "-bench" || flag == "-test.bench" || flag == "-fuzz" || flag == "-test.fuzz" {
				return "", nil, fmt.Errorf("Go example splitting does not support benchmarks or active fuzzing")
			}
		}
		switch filepath.Base(args[0]) {
		case "go":
			if len(args) < 2 || args[1] != "test" {
				return "", nil, fmt.Errorf("Go example splitting requires go test or gotestsum")
			}
		case "gotestsum":
			if !slices.Contains(args, "--") {
				index := slices.Index(args, packages[0])
				args = slices.Insert(args, index, "--")
			}
		default:
			return "", nil, fmt.Errorf("Go example splitting requires go test or gotestsum")
		}
		args = append(args, "-run=^("+strings.Join(names, "|")+")$")
	}

	return args[0], args[1:], nil
}

func hasGoTestJSONArg(args []string) bool {
	for _, arg := range args {
		if arg == "-json" {
			return true
		}
	}
	return false
}

func goJSONLFileArg(args []string) string {
	for i, arg := range args {
		if arg == "--jsonfile" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--jsonfile=") {
			return strings.TrimPrefix(arg, "--jsonfile=")
		}
	}
	return ""
}

func isGoTestJSONCommand(args []string) bool {
	return len(args) >= 3 && filepath.Base(args[0]) == "go" && args[1] == "test" && hasGoTestJSONArg(args)
}

// Pluck unique packages from test cases
func (g GoTest) getPackages(testCases []plan.TestCase) ([]string, error) {
	packages := make([]string, 0, len(testCases))

	packagesSeen := map[string]bool{}
	for _, tc := range testCases {
		packageName := packageFromTestCase(tc)
		if packageName == "" {
			continue
		}
		if !packagesSeen[packageName] {
			packages = append(packages, packageName)
			packagesSeen[packageName] = true
		}
	}
	if len(packages) == 0 {
		// The likelihood of this is very low
		return nil, fmt.Errorf("unable to extract package names from test plan")
	}
	debug.Printf("Packages: %v\n", packages)

	return packages, nil
}

func packageFromTestCase(tc plan.TestCase) string {
	if tc.Format == plan.TestCaseFormatSelector && tc.Value != "" {
		return tc.Value
	}
	if tc.Value != "" && tc.Path == "" {
		return tc.Value
	}
	return tc.Path
}
