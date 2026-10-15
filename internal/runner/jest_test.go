package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/assert"
)

func TestNewJest(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		//default
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:            "npx jest --runTestsByPath {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}",
				TestFilePattern:        "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}",
				TestFileExcludePattern: "",
				RetryTestCommand:       "npx jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
		},
		// custom
		{
			input: RunnerConfig{
				TestCommand:            "yarn test --json --outputFile {{resultPath}}",
				TestFilePattern:        "spec/models/**/*.spec.js",
				TestFileExcludePattern: "spec/features/**/*.spec.js",
				RetryTestCommand:       "yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
			want: RunnerConfig{
				TestCommand:            "yarn test --json --outputFile {{resultPath}}",
				TestFilePattern:        "spec/models/**/*.spec.js",
				TestFileExcludePattern: "spec/features/**/*.spec.js",
				RetryTestCommand:       "yarn test --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}",
			},
		},
	}

	for _, c := range cases {
		got := NewJest(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want, cmp.AllowUnexported(RunnerConfig{})); diff != "" {
			t.Errorf("NewJest(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestJestRun(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/spells/expelliarmus.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run(%v) error = %v", testCases, err)
	}

	if len(result.tests) != 1 {
		t.Errorf("Rspec.Run(%v) len(RunResult.tests) = %d, want 1", testCases, len(result.tests))
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestJestRun_Retry(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand:      "jest --invalid-option --json --outputFile {{resultPath}}",
		RetryTestCommand: "npx jest --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}} ./testdata/jest/spells/expelliarmus.spec.js ./testdata/jest/failure.spec.js",
		ResultPath:       "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Scope: "expelliarmus", Name: "disarms the opponent"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, true)

	if err != nil {
		t.Errorf("Jest.Run(%v) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestJestRun_Retry_WithTestFilePattern(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand:      "jest --invalid-option --json --outputFile {{resultPath}}",
		RetryTestCommand: "npx jest {{testExamples}} --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}}",
		ResultPath:       "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Scope: "expelliarmus", Name: "disarms the opponent", Path: "./testdata/jest/spells/expelliarmus.spec.js"},
		{Scope: "this will fail", Name: "for sure", Path: "./testdata/jest/failure.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	jest.Run(result, testCases, true)

	if got := len(result.tests); got != 2 {
		t.Errorf("Jest.Run(%v) test count = %d, want %d", testCases, got, 2)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
}

func TestJestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/failure.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	wantFailedTests := []plan.TestCase{
		{
			Scope: "this will fail",
			Name:  "for sure",
			Path:  "failure.spec.js",
		},
	}

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusFailed {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Jest.Run(%v) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestJestRun_TestSkipped(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/skipped.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Jest.Run(%v) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	test := result.tests["this will be skipped/for sure/skipped.spec.js"]
	if test.Status != TestStatusSkipped {
		t.Errorf("Jest.Run(%v) test.Status = %v, want %v", testCases, test.Status, TestStatusSkipped)
	}

	todoTest := result.tests["this will be skipped/todo yeah/skipped.spec.js"]
	if todoTest.Status != TestStatusSkipped {
		t.Errorf("Jest.Run(%v) todoTest.Status = %v, want %v", testCases, todoTest.Status, TestStatusSkipped)
	}
}

func TestJestRun_RuntimeError(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --json --outputFile {{resultPath}}",
		ResultPath:  "jest.json",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/spells/expelliarmus.spec.js"},
		{Path: "./testdata/jest/runtimeError.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	// Make sure that we capture other tests that are not affected by the runtime error (expelliarmus.spec.js)
	if len(result.tests) != 1 {
		t.Errorf("Jest.Run(%v) len(RunResult.tests) = %d, want 1", testCases, len(result.tests))
	}

	if result.Status() != RunStatusError {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}

	// Only the file errored, so the other files' failed tests can be retried.
	if got, want := result.Error().Error(), "Test files failed to run: runtimeError.spec.js"; got != want {
		t.Errorf("Jest.Run(%v) RunResult.Error = %q, want %q", testCases, got, want)
	}
	if !result.Retryable() {
		t.Errorf("Jest.Run(%v) RunResult.Retryable = false, want true", testCases)
	}
}

// This test ensures that when a test fails due to timeout and passes after retry, the run result should be passed.
// It covers the scenario where the test location is missing when it timed out, but set properly when passed, resulting in the test
// being treated as different tests.
func TestJestRun_TestTimeoutWithRetry(t *testing.T) {
	changeCwd(t, "./testdata/jest")

	jest := NewJest(RunnerConfig{
		TestCommand:      "npx jest --json --outputFile {{resultPath}} --testTimeout=1",
		ResultPath:       "jest.json",
		RetryTestCommand: "npx jest --testNamePattern '{{testNamePattern}}' --json --outputFile {{resultPath}} ./testdata/jest/slow.spec.js",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/jest/slow.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusFailed {
		t.Errorf("Jest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusFailed)
	}

	// after the test failed due to timeout, retry the test that will eventually passed
	failedTests := result.FailedTests()
	err = jest.Run(result, failedTests, true)

	if err != nil {
		t.Errorf("Jest.Run() error = %v", err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Jest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusPassed)
	}
}

func TestJestRun_CommandFailed(t *testing.T) {
	jest := NewJest(RunnerConfig{
		TestCommand: "npx jest --invalid-option --outputFile {{resultPath}}",
	})

	t.Cleanup(func() {
		os.Remove(jest.ResultPath)
	})

	testCases := []plan.TestCase{}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)
}

func TestJestRun_SignaledError(t *testing.T) {
	jest := NewJest(RunnerConfig{
		TestCommand: "./testdata/segv.sh --outputFile {{resultPath}}",
	})

	testCases := []plan.TestCase{
		{Path: "./doesnt-matter.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := jest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Jest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Jest.Run(%v) error type = %T (%v), want *ErrProcessSignaled", testCases, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Jest.Run(%v) signal = %d, want %d", testCases, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestJestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{{Path: "spec/user.spec.js"}, {Path: "spec/billing.spec.js"}}
	testCommand := "jest {{testExamples}} --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Errorf("commandNameAndArgs(%v, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"spec/billing.spec.js", "spec/user.spec.js", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestJestCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{{Path: "spec/user.spec.js"}, {Path: "spec/billing.spec.js"}}
	testCommand := "jest --json --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Errorf("commandNameAndArgs(%v, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--json", "--outputFile", "jest.json", "spec/billing.spec.js", "spec/user.spec.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestJestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []plan.TestCase{{Path: "spec/user.spec.js"}, {Path: "spec/billing.spec.js"}}
	testCommand := "jest --options '{{testExamples}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, false)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if !errors.Is(err, shellquote.UnterminatedSingleQuoteError) {
		t.Errorf("commandNameAndArgs() error = %v, want %v", err, shellquote.UnterminatedSingleQuoteError)
	}
}

func TestJestCommandNameAndArgs_WithSpecialCharactersInPath(t *testing.T) {
	// Test paths with special regex characters like parentheses (Next.js route groups)
	// and square brackets (Next.js dynamic routes). These are passed as-is because
	// the default TestCommand uses --runTestsByPath which treats args as literal paths.
	testCases := []plan.TestCase{
		{Path: "src/app/(main)/page.test.tsx"},
		{Path: "src/app/(main)/[catalogId]/product.test.tsx"},
	}
	testCommand := "jest --runTestsByPath {{testExamples}} --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Errorf("commandNameAndArgs(%v, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{
		"--runTestsByPath",
		"src/app/(main)/[catalogId]/product.test.tsx",
		"src/app/(main)/page.test.tsx",
		"--outputFile",
		"jest.json",
	}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestJestRetryCommandNameAndArgs_HappyPath(t *testing.T) {
	testCases := []plan.TestCase{
		{
			Scope: "this",
			Name:  "will fail",
			Path:  "spec/user.spec.js",
		},
		{
			Scope: "this",
			Name:  "other one will fail",
			Path:  "spec/billing.spec.js",
		},
	}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, true)
	if err != nil {
		t.Errorf("CommandNameAndArgs(%v, %v) error = %v", testCases, true, err)
	}

	wantName := "jest"
	wantArgs := []string{"--testNamePattern", "^(this will fail|this other one will fail)$", "--json", "--testLocationInResults", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestJestRetryCommandNameAndArgs_WithSpecialCharacters(t *testing.T) {
	testCases := []plan.TestCase{
		{
			Scope: "test",
			Name:  "with special characters .+*?()|[]{}^$",
			Path:  "spec/user.spec.js",
		},
		{
			Scope: "another",
			Name:  "test",
			Path:  "spec/billing.spec.js",
		},
		{
			Name: "top-level test",
			Path: "spec/billing.spec.js",
		},
	}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, true)
	if err != nil {
		t.Errorf("CommandNameAndArgs(%v, %q) error = %v", testCases, retryTestCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{"--testNamePattern", `^(test with special characters \.\+\*\?\(\)\|\[\]\{\}\^\$|another test|top-level test)$`, "--json", "--testLocationInResults", "--outputFile", "jest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestJestRetryCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{
		{
			Scope: "this",
			Name:  "will fail",
			Path:  "spec/user.spec.js",
		},
		{
			Scope: "this",
			Name:  "other one will fail",
			Path:  "spec/billing.spec.js",
		},
	}
	retryTestCommand := "jest --json --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, true)
	fmt.Println(err)

	wantName := ""
	wantArgs := []string{}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs() diff (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs() diff (-got +want):\n%s", diff)
	}

	desiredString := "couldn't find '{{testNamePattern}}' sentinel in retry command"
	if err.Error() != desiredString {
		t.Errorf("CommandNameAndArgs() error = %v, want %v", err, desiredString)
	}
}

func TestJestRetryCommandNameAndArgs_WithTestExamplesPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{
		{
			Scope: "this",
			Name:  "will fail",
			Path:  "spec/user.spec.js",
		},
		{
			Scope: "this",
			Name:  "other one will fail",
			Path:  "spec/billing.spec.js",
		},
	}
	retryTestCommand := "jest --testNamePattern '{{testNamePattern}}' {{testExamples}} --json --testLocationInResults --outputFile {{resultPath}}"

	jest := NewJest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "jest.json",
	})

	gotName, gotArgs, err := jest.CommandNameAndArgs(testCases, true)
	if err != nil {
		t.Errorf("CommandNameAndArgs(%v, %q) error = %v", testCases, retryTestCommand, err)
	}

	wantName := "jest"
	wantArgs := []string{
		"--testNamePattern",
		"^(this will fail|this other one will fail)$",
		"spec/billing.spec.js",
		"spec/user.spec.js",
		"--json",
		"--testLocationInResults",
		"--outputFile",
		"jest.json",
	}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestJestDiscoverTestTargets(t *testing.T) {
	changeCwd(t, "./testdata/jest")
	jest := NewJest(RunnerConfig{})

	got, err := jest.DiscoverTestTargets()
	if err != nil {
		t.Errorf("Jest.DiscoverTestTargets() error = %v", err)
	}

	want := []string{
		"failure.spec.js",
		"runtimeError.spec.js",
		"skipped.spec.js",
		"slow.spec.js",
		"spells/expelliarmus.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Jest.DiscoverTestTargets() diff (-got +want):\n%s", diff)
	}
}

func TestParseNativeJestReport(t *testing.T) {
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseNativeReport("jest-json", []byte(`{"testResults":[
		{"name":"`+workDir+`/src/a.test.js","status":"failed","assertionResults":[
			{"title":"adds","ancestorTitles":["math","sum"],"status":"failed","location":{"line":3,"column":5}},
			{"title":"each","ancestorTitles":[],"status":"failed"},
			{"title":"each","ancestorTitles":[],"status":"passed"},
			{"title":"later","ancestorTitles":[],"status":"todo"}
		]},
		{"name":"b.test.js","status":"passed","assertionResults":[]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := ParsedReport{Tests: []ReportedTest{
		{TestCase: plan.TestCase{Path: "src/a.test.js", Scope: "math sum", Name: "adds"}, Status: TestStatusFailed, Selector: "src/a.test.js", Location: "src/a.test.js:3"},
		// Same-named tests in one file are reported once, failed if any failed.
		{TestCase: plan.TestCase{Path: "src/a.test.js", Name: "each"}, Status: TestStatusFailed, Selector: "src/a.test.js"},
		{TestCase: plan.TestCase{Path: "src/a.test.js", Name: "later"}, Status: TestStatusSkipped, Selector: "src/a.test.js"},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("parsed report (-want +got):\n%s", diff)
	}

	// A file that fails without a failed test, such as one that didn't load,
	// failed outside its tests. Other files are unaffected.
	got, err = ParseNativeReport("jest-json", []byte(`{"numRuntimeErrorTestSuites":2,"testResults":[
		{"name":"c.test.js","status":"failed","assertionResults":[{"title":"t","ancestorTitles":[],"status":"passed"}]},
		{"name":"d.test.js","status":"failed","assertionResults":[]},
		{"name":"e.test.js","status":"passed","assertionResults":[{"title":"t","ancestorTitles":[],"status":"passed"}]}
	]}`))
	if err != nil || got.ErrorsOutsideTests || !reflect.DeepEqual(got.ErroredFiles, []string{"c.test.js", "d.test.js"}) {
		t.Fatalf("report=%+v err=%v, want only c.test.js and d.test.js errored", got, err)
	}

	// A file whose tests failed can also fail outside them, such as in an
	// afterAll hook. Jest counts it but doesn't name it, so the whole report
	// errors rather than losing the error.
	got, err = ParseNativeReport("jest-json", []byte(`{"numRuntimeErrorTestSuites":1,"testResults":[
		{"name":"f.test.js","status":"failed","assertionResults":[{"title":"t","ancestorTitles":[],"status":"failed"}]},
		{"name":"g.test.js","status":"failed","assertionResults":[{"title":"t","ancestorTitles":[],"status":"failed"}]}
	]}`))
	if err != nil || !got.ErrorsOutsideTests {
		t.Fatalf("report=%+v err=%v, want errors outside tests", got, err)
	}

	for _, invalid := range []string{`{}`, `{"testResults":[{"name":"c.test.js","assertionResults":[{"title":"t","status":"focused"}]}]}`} {
		if _, err := ParseNativeReport("jest-json", []byte(invalid)); err == nil {
			t.Errorf("accepted invalid report %s", invalid)
		}
	}
}
