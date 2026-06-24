package runner

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/kballard/go-shellquote"
)

type GoTest struct {
	RunnerConfig
	resultFormat string
}

// Compile-time check that GoTest implements TestRunner
var _ TestRunner = (*GoTest)(nil)

func NewGoTest(c RunnerConfig) GoTest {
	if c.TestCommand == "" {
		c.TestCommand = "gotestsum --junitfile={{resultPath}} {{packages}}"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	resultFormat := "junit"
	if commandUploadsGoJSONL(c.TestCommand) {
		resultFormat = "go-jsonl"
	}

	return GoTest{
		RunnerConfig: c,
		resultFormat: resultFormat,
	}
}

func (g GoTest) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     false,
		SplitByExample:  false,
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

func (g GoTest) ResultFilePath() string {
	if g.resultFormat == "go-jsonl" {
		return g.goJSONLResultPathFromCommand()
	}
	return g.RunnerConfig.ResultFilePath()
}

func (g GoTest) goJSONLResultPath() string {
	return g.ResultPath + ".jsonl"
}

func (g GoTest) goJSONLResultPathFromCommand() string {
	args, err := g.commandArgsWithoutPackages(g.TestCommand)
	if err != nil {
		return g.ResultPath
	}

	if path := goJSONLFileArg(args); path != "" {
		return path
	}

	return g.ResultPath
}

func commandUploadsGoJSONL(command string) bool {
	args, err := shellquote.Split(command)
	if err != nil {
		return strings.Contains(command, "--jsonfile") || strings.Contains(command, "go test -json")
	}
	return isGoTestJSONCommand(args) || goJSONLFileArg(args) != ""
}

func (g GoTest) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in go test")
}

// Run executes the configured command for the specified packages.
func (g GoTest) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := buildCommand(g, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := runAndForwardSignal(cmd)

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

func (g GoTest) parseResults(result *RunResult) error {
	switch g.resultFormat {
	case "junit":
		return g.parseJUnitResults(result)
	case "go-jsonl":
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
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

func (g GoTest) parseGoJSONLResults(result *RunResult) error {
	events, err := loadGoJSONLTestEvents(g.ResultFilePath())
	if err != nil {
		return err
	}

	packageOutputs := map[string]string{}
	packageFailed := map[string]bool{}
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
			}
			continue
		}

		status, ok := goJSONLActionStatus(event.Action)
		if !ok {
			continue
		}

		key := event.Package + "/" + event.Test
		testStatuses[key] = status
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
		if packageOutputs[pkg] != "" && strings.Contains(packageOutputs[pkg], "[build failed]") {
			result.error = fmt.Errorf("go test failed to build %s", pkg)
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

// GetFiles discovers Go packages using `go list ./...`.
// Note that "file" does not exist as a first level concept in Golang projects
// So this func is returning a list of packages instead of files.
// The implication is that the Server-side smart test splitting will never work.
// It almost will always fallback to simple splitting.
func (g GoTest) GetFiles() ([]string, error) {
	debug.Println("Discovering Go packages with `go list ./...`")
	cmd := exec.Command("go", "list", "./...")
	output, err := cmd.Output()
	if err != nil {
		// Handle stderr for better error messages
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list failed: %w\nstderr:\n%s", err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("failed to run go list: %w", err)
	}
	packages := strings.Split(strings.TrimSpace(string(output)), "\n")
	// Filter out empty strings if any
	validPackages := []string{}
	for _, pkg := range packages {
		if pkg != "" {
			validPackages = append(validPackages, pkg)
		}
	}
	debug.Println("Discovered", len(validPackages), "packages")
	if len(validPackages) == 0 {
		return nil, fmt.Errorf("no Go packages found using `go list ./...`")
	}
	return validPackages, nil
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

	args, err := g.commandArgsWithoutPackages(cmd)

	if err != nil {
		return "", []string{}, err
	}
	args = g.withGoJSONLStdoutCapture(args)

	return args[0], args[1:], nil
}

func (g GoTest) commandArgsWithoutPackages(cmd string) ([]string, error) {
	cmd = strings.Replace(cmd, "{{resultPath}}", g.ResultPath, 1)
	cmd = strings.Replace(cmd, "{{goJsonlResultPath}}", g.goJSONLResultPath(), 1)
	return shellquote.Split(cmd)
}

func (g GoTest) withGoJSONLStdoutCapture(args []string) []string {
	if !isGoTestJSONCommand(args) || goJSONLFileArg(args) != "" {
		return args
	}

	wrapped := []string{"-o", "pipefail", "-c", `"$@" | tee "$0"`, g.ResultPath}
	wrapped = append(wrapped, args...)
	return append([]string{"bash"}, wrapped...)
}

func hasGoJSONLArg(args []string) bool {
	return isGoTestJSONCommand(args) || goJSONLFileArg(args) != ""
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
		packageName := tc.Path
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
