package runner

import (
	"errors"
	"os"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
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
				TestCommand:            "npx vitest run {{testExamples}} --reporter=json --outputFile {{resultPath}}",
				TestFilePattern:        "**/{__tests__/**/*,*.spec,*.test}.{ts,js,tsx,jsx}",
				TestFileExcludePattern: "",
				RetryTestCommand:       "npx vitest run --testNamePattern '{{testNamePattern}}' --reporter=json --outputFile {{resultPath}}",
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

func TestVitestGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/vitest")
	vitest := NewVitest(RunnerConfig{})

	got, err := vitest.GetFiles()
	if err != nil {
		t.Errorf("Vitest.GetFiles() error = %v", err)
	}

	want := []string{
		"failure.spec.js",
		"skipped.spec.js",
		"spells/expelliarmus.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Vitest.GetFiles() diff (-got +want):\n%s", diff)
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
