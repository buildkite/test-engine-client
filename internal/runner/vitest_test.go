package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestNewVitest(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		// default
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:            "npx vitest run {{testExamples}} --reporter=default --reporter=json --outputFile {{resultPath}}",
				TestFilePattern:        "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}",
				TestFileExcludePattern: "",
				RetryTestCommand:       "npx vitest run --testNamePattern '{{testNamePattern}}' --reporter=default --reporter=json --outputFile {{resultPath}}",
			},
		},
		// custom
		{
			input: RunnerConfig{
				TestCommand:            "yarn vitest run --reporter=json --outputFile {{resultPath}}",
				TestFilePattern:        "src/**/*.spec.ts",
				TestFileExcludePattern: "src/e2e/**/*.spec.ts",
				RetryTestCommand:       "yarn vitest run --testNamePattern '{{testNamePattern}}' --reporter=json --outputFile {{resultPath}}",
			},
			want: RunnerConfig{
				TestCommand:            "yarn vitest run --reporter=json --outputFile {{resultPath}}",
				TestFilePattern:        "src/**/*.spec.ts",
				TestFileExcludePattern: "src/e2e/**/*.spec.ts",
				RetryTestCommand:       "yarn vitest run --testNamePattern '{{testNamePattern}}' --reporter=json --outputFile {{resultPath}}",
			},
		},
	}

	for _, c := range cases {
		got := NewVitest(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want, cmp.AllowUnexported(RunnerConfig{})); diff != "" {
			t.Errorf("NewVitest(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestVitestDiscoverTestTargets(t *testing.T) {
	changeCwd(t, "./testdata/vitest")
	vitest := NewVitest(RunnerConfig{})

	got, err := vitest.DiscoverTestTargets()
	if err != nil {
		t.Errorf("Vitest.DiscoverTestTargets() error = %v", err)
	}

	want := []string{
		"failure.spec.js",
		"skipped.spec.js",
		"spells/expelliarmus.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Vitest.DiscoverTestTargets() diff (-got +want):\n%s", diff)
	}
}

func TestVitestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{{Path: "src/user.spec.ts"}, {Path: "src/billing.spec.ts"}}
	testCommand := "vitest run {{testExamples}} --reporter=json --outputFile {{resultPath}}"

	vitest := NewVitest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "vitest.json",
	})

	gotName, gotArgs, err := vitest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Errorf("CommandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "vitest"
	wantArgs := []string{"run", "src/billing.spec.ts", "src/user.spec.ts", "--reporter=json", "--outputFile", "vitest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestVitestRetryCommandNameAndArgs_HappyPath(t *testing.T) {
	testCases := []plan.TestCase{
		{Scope: "this", Name: "will fail", Path: "src/user.spec.ts"},
		{Scope: "this", Name: "other one will fail", Path: "src/billing.spec.ts"},
	}
	retryTestCommand := "vitest run --testNamePattern '{{testNamePattern}}' --reporter=json --outputFile {{resultPath}}"

	vitest := NewVitest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "vitest.json",
	})

	gotName, gotArgs, err := vitest.CommandNameAndArgs(testCases, true)
	if err != nil {
		t.Errorf("CommandNameAndArgs(%q, %v) error = %v", testCases, true, err)
	}

	wantName := "vitest"
	wantArgs := []string{"run", "--testNamePattern", "(this will fail|this other one will fail)", "--reporter=json", "--outputFile", "vitest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
}

func TestVitestRetryCommandNameAndArgs_WithoutInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{{Scope: "this", Name: "will fail", Path: "src/user.spec.ts"}}
	retryTestCommand := "vitest run --reporter=json --outputFile {{resultPath}}"

	vitest := NewVitest(RunnerConfig{
		RetryTestCommand: retryTestCommand,
		ResultPath:       "vitest.json",
	})

	_, _, err := vitest.CommandNameAndArgs(testCases, true)

	want := "couldn't find '{{testNamePattern}}' sentinel in retry command"
	if err == nil || err.Error() != want {
		t.Errorf("CommandNameAndArgs() error = %v, want %v", err, want)
	}
}

func TestVitestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []plan.TestCase{{Path: "src/user.spec.ts"}}
	testCommand := "vitest run --options '{{testExamples}}"

	vitest := NewVitest(RunnerConfig{TestCommand: testCommand})

	_, _, err := vitest.CommandNameAndArgs(testCases, false)
	if !errors.Is(err, shellquote.UnterminatedSingleQuoteError) {
		t.Errorf("CommandNameAndArgs() error = %v, want %v", err, shellquote.UnterminatedSingleQuoteError)
	}
}

// TestVitestRun is an integration test that requires vitest to be installed in
// the testdata fixture (via `yarn install` in internal/runner/testdata, as CI
// does for the other JS runners).
func TestVitestRun(t *testing.T) {
	changeCwd(t, "./testdata/vitest")

	vitest := NewVitest(RunnerConfig{
		TestCommand: "npx vitest run --reporter=json --outputFile {{resultPath}}",
		ResultPath:  "vitest.json",
	})
	t.Cleanup(func() { os.Remove(vitest.ResultPath) })

	testCases := []plan.TestCase{{Path: "./spells/expelliarmus.spec.js"}}
	result := NewRunResult([]plan.TestCase{})
	if err := vitest.Run(result, testCases, false); err != nil {
		t.Errorf("Vitest.Run(%q) error = %v", testCases, err)
	}
	if len(result.tests) != 1 {
		t.Errorf("Vitest.Run(%q) len(RunResult.tests) = %d, want 1", testCases, len(result.tests))
	}
	if result.Status() != RunStatusPassed {
		t.Errorf("Vitest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

// TestVitestRun_TestSkipped guards the Vitest-specific status mapping: Vitest
// reports skipped/todo tests with status "skipped"/"todo", whereas Jest uses
// "pending". Both must map to TestStatusSkipped.
func TestVitestRun_TestSkipped(t *testing.T) {
	changeCwd(t, "./testdata/vitest")

	vitest := NewVitest(RunnerConfig{
		TestCommand: "npx vitest run --reporter=json --outputFile {{resultPath}}",
		ResultPath:  "vitest.json",
	})
	t.Cleanup(func() { os.Remove(vitest.ResultPath) })

	testCases := []plan.TestCase{{Path: "./skipped.spec.js"}}
	result := NewRunResult([]plan.TestCase{})
	if err := vitest.Run(result, testCases, false); err != nil {
		t.Errorf("Vitest.Run(%q) error = %v", testCases, err)
	}
	if result.Status() != RunStatusPassed {
		t.Errorf("Vitest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	skipped := result.tests["this will be skipped/for sure/skipped.spec.js"]
	if skipped.Status != TestStatusSkipped {
		t.Errorf("Vitest.Run(%q) skipped test status = %v, want %v", testCases, skipped.Status, TestStatusSkipped)
	}
	todo := result.tests["this will be skipped/todo yeah/skipped.spec.js"]
	if todo.Status != TestStatusSkipped {
		t.Errorf("Vitest.Run(%q) todo test status = %v, want %v", testCases, todo.Status, TestStatusSkipped)
	}
}

func TestVitestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/vitest")

	vitest := NewVitest(RunnerConfig{
		TestCommand: "npx vitest run --reporter=json --outputFile {{resultPath}}",
		ResultPath:  "vitest.json",
	})
	t.Cleanup(func() { os.Remove(vitest.ResultPath) })

	testCases := []plan.TestCase{{Path: "./failure.spec.js"}}
	result := NewRunResult([]plan.TestCase{})
	vitest.Run(result, testCases, false)

	wantFailedTests := []plan.TestCase{
		{Scope: "this will fail", Name: "for sure", Path: "failure.spec.js"},
	}
	if result.Status() != RunStatusFailed {
		t.Errorf("Vitest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Vitest.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestVitestRun_RetryIgnoresSkippedResultsForTestsNotRetried(t *testing.T) {
	workDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	testFile := filepath.Join(workDir, "multi.spec.js")
	tempDir := t.TempDir()
	firstReportPath := filepath.Join(tempDir, "first.json")
	retryReportPath := filepath.Join(tempDir, "retry.json")
	resultPath := filepath.Join(tempDir, "vitest.json")

	firstReport := fmt.Sprintf(`{
  "testResults": [{
    "name": %q,
    "status": "failed",
    "assertionResults": [
      {"status": "passed", "title": "passes", "ancestorTitles": ["multi"]},
      {"status": "failed", "title": "fails", "ancestorTitles": ["multi"]}
    ]
  }]
}`, testFile)
	retryReport := fmt.Sprintf(`{
  "testResults": [{
    "name": %q,
    "status": "failed",
    "assertionResults": [
      {"status": "skipped", "title": "passes", "ancestorTitles": ["multi"]},
      {"status": "failed", "title": "fails", "ancestorTitles": ["multi"]}
    ]
  }]
}`, testFile)

	if err := os.WriteFile(firstReportPath, []byte(firstReport), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", firstReportPath, err)
	}
	if err := os.WriteFile(retryReportPath, []byte(retryReport), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", retryReportPath, err)
	}

	vitest := NewVitest(RunnerConfig{
		TestCommand:      shellquote.Join("sh", "-c", `cp "$1" "$2"`, "sh", firstReportPath, "{{resultPath}}"),
		RetryTestCommand: shellquote.Join("sh", "-c", `cp "$1" "$2"`, "sh", retryReportPath, "{{resultPath}}", "{{testNamePattern}}"),
		ResultPath:       resultPath,
	})

	result := NewRunResult([]plan.TestCase{})
	testCases := []plan.TestCase{{Path: "multi.spec.js"}}
	if err := vitest.Run(result, testCases, false); err != nil {
		t.Fatalf("Vitest.Run(%q, retry=false) error = %v", testCases, err)
	}

	failedTests := result.FailedTests()
	wantFailedTests := []plan.TestCase{{Scope: "multi", Name: "fails", Path: "multi.spec.js"}}
	if diff := cmp.Diff(failedTests, wantFailedTests); diff != "" {
		t.Fatalf("Vitest.Run(%q, retry=false) FailedTests() diff (-got +want):\n%s", testCases, diff)
	}

	if err := vitest.Run(result, failedTests, true); err != nil {
		t.Fatalf("Vitest.Run(%q, retry=true) error = %v", failedTests, err)
	}

	passedTest := result.tests[testIdentifier(plan.TestCase{Scope: "multi", Name: "passes", Path: "multi.spec.js"})]
	if passedTest.Status != TestStatusPassed {
		t.Errorf("Vitest.Run(%q, retry=true) passed test status = %v, want %v", failedTests, passedTest.Status, TestStatusPassed)
	}
	if passedTest.ExecutionCount != 1 {
		t.Errorf("Vitest.Run(%q, retry=true) passed test execution count = %d, want 1", failedTests, passedTest.ExecutionCount)
	}

	failedTest := result.tests[testIdentifier(plan.TestCase{Scope: "multi", Name: "fails", Path: "multi.spec.js"})]
	if failedTest.Status != TestStatusFailed {
		t.Errorf("Vitest.Run(%q, retry=true) failed test status = %v, want %v", failedTests, failedTest.Status, TestStatusFailed)
	}
	if failedTest.ExecutionCount != 2 {
		t.Errorf("Vitest.Run(%q, retry=true) failed test execution count = %d, want 2", failedTests, failedTest.ExecutionCount)
	}
}
