package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/kballard/go-shellquote"
)

type GoTest struct {
	RunnerConfig
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

	return GoTest{
		RunnerConfig: c,
	}
}

func (g GoTest) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     false,
		SplitByExample:  false,
		FilterTestFiles: false,
		AutoRetry:       true,
		Mute:            true,
		Skip:            false,
	}
}

func (g GoTest) Name() string {
	return "gotest"
}

func (g GoTest) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in go test")
}

// Run executes the configured command for the specified packages.
func (g GoTest) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmdName, cmdArgs, err := g.commandNameAndArgs(g.TestCommand, testCases)
	if err != nil {
		return fmt.Errorf("failed to build command: %w", err)
	}

	cmd := exec.Command(cmdName, cmdArgs...)
	err = runAndForwardSignal(cmd)
	// go test output does not differentiate build fail or test fail. They both return 1
	// What is even more bizarre is that even when go test failed on compliation, it will still generate an output xml
	// file that says "TestMain" failed..
	if exitError := new(exec.ExitError); errors.As(err, &exitError) && exitError.ExitCode() != 1 {
		return err
	}

	testResults, err := loadAndParseGotestJUnitXmlResult(g.ResultPath)

	if err != nil {
		return fmt.Errorf("failed to load and parse test result: %w", err)
	}

	for _, test := range testResults {
		result.RecordTestResult(plan.TestCase{
			Format: plan.TestCaseFormatExample,
			Scope:  test.Classname,
			Name:   test.Name,
			// This is the special thing about go test support.
			Path: test.Classname,
		}, test.Result)
	}

	return nil // Success
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

func (p GoTest) commandNameAndArgs(cmd string, testCases []plan.TestCase) (string, []string, error) {
	packages, err := p.getPackages(testCases)
	if err != nil {
		return "", []string{}, nil
	}

	concatenatedPackages := strings.Join(packages, " ")

	if strings.Contains(cmd, "{{packages}}") {
		cmd = strings.Replace(cmd, "{{packages}}", concatenatedPackages, 1)
	} else {
		cmd = cmd + " " + concatenatedPackages
	}

	cmd = strings.Replace(cmd, "{{resultPath}}", p.ResultPath, 1)

	args, err := shellquote.Split(cmd)

	if err != nil {
		return "", []string{}, err
	}

	return args[0], args[1:], nil
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
