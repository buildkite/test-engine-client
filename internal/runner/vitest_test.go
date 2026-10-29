package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/require"
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
		t.Errorf("CommandNameAndArgs(%v, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "vitest"
	wantArgs := []string{"run", "src/billing.spec.ts", "src/user.spec.ts", "--reporter=json", "--outputFile", "vitest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
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
		t.Errorf("CommandNameAndArgs(%v, %v) error = %v", testCases, true, err)
	}

	wantName := "vitest"
	wantArgs := []string{"run", "--testNamePattern", "^(this will fail|this other one will fail)$", "--reporter=json", "--outputFile", "vitest.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("CommandNameAndArgs(%v, %q) diff (-got +want):\n%s", testCases, retryTestCommand, diff)
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
		t.Errorf("Vitest.Run(%v) error = %v", testCases, err)
	}
	if len(result.tests) != 1 {
		t.Errorf("Vitest.Run(%v) len(RunResult.tests) = %d, want 1", testCases, len(result.tests))
	}
	if result.Status() != RunStatusPassed {
		t.Errorf("Vitest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
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
		t.Errorf("Vitest.Run(%v) error = %v", testCases, err)
	}
	if result.Status() != RunStatusPassed {
		t.Errorf("Vitest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	skipped := result.tests["this will be skipped/for sure/skipped.spec.js"]
	if skipped.Status != TestStatusSkipped {
		t.Errorf("Vitest.Run(%v) skipped test status = %v, want %v", testCases, skipped.Status, TestStatusSkipped)
	}
	todo := result.tests["this will be skipped/todo yeah/skipped.spec.js"]
	if todo.Status != TestStatusSkipped {
		t.Errorf("Vitest.Run(%v) todo test status = %v, want %v", testCases, todo.Status, TestStatusSkipped)
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
		t.Errorf("Vitest.Run(%v) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Vitest.Run(%v) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
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
	require.ErrorContains(t, vitest.Run(result, testCases, false), "Vitest finished with failed tests")

	failedTests := result.FailedTests()
	wantFailedTests := []plan.TestCase{{Scope: "multi", Name: "fails", Path: "multi.spec.js"}}
	if diff := cmp.Diff(failedTests, wantFailedTests); diff != "" {
		t.Fatalf("Vitest.Run(%v, retry=false) FailedTests() diff (-got +want):\n%s", testCases, diff)
	}

	require.ErrorContains(t, vitest.Run(result, failedTests, true), "Vitest finished with failed tests")

	passedTest := result.tests[testIdentifier(plan.TestCase{Scope: "multi", Name: "passes", Path: "multi.spec.js"})]
	if passedTest.Status != TestStatusPassed {
		t.Errorf("Vitest.Run(%v, retry=true) passed test status = %v, want %v", failedTests, passedTest.Status, TestStatusPassed)
	}
	if passedTest.ExecutionCount != 1 {
		t.Errorf("Vitest.Run(%v, retry=true) passed test execution count = %d, want 1", failedTests, passedTest.ExecutionCount)
	}

	failedTest := result.tests[testIdentifier(plan.TestCase{Scope: "multi", Name: "fails", Path: "multi.spec.js"})]
	if failedTest.Status != TestStatusFailed {
		t.Errorf("Vitest.Run(%v, retry=true) failed test status = %v, want %v", failedTests, failedTest.Status, TestStatusFailed)
	}
	if failedTest.ExecutionCount != 2 {
		t.Errorf("Vitest.Run(%v, retry=true) failed test execution count = %d, want 2", failedTests, failedTest.ExecutionCount)
	}
}

func TestVitestRun_RetryNamePattern(t *testing.T) {
	testFile, err := filepath.Abs("multi.spec.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		ancestors []string
		title     string
		scope     string
		matches   []string
		misses    []string
	}{
		{
			name:      "nested suites with spaces and regex characters",
			ancestors: []string{"outer suite [1]", "inner suite (2)"},
			title:     "works with a+b?", scope: "outer suite [1] inner suite (2)",
			matches: []string{"outer suite [1] inner suite (2) works with a+b?", "outer suite [1] > inner suite (2) > works with a+b?"},
			misses:  []string{"outer > suite [1] > inner > suite (2) > works with a+b?", "outer suite 1 > inner suite 2 > works with ab"},
		},
		{
			name: "no scope", title: "works [1]", matches: []string{"works [1]"},
			misses: []string{" works [1]", " > works [1]", "works 1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			reportPath := filepath.Join(dir, "vitest.json")
			// fullName stays space-separated even when Vitest 5 matches names with " > ".
			report := map[string]any{"testResults": []any{map[string]any{
				"name": testFile, "assertionResults": []any{map[string]any{
					"status": "failed", "title": tc.title, "ancestorTitles": tc.ancestors, "fullName": tc.matches[0],
				}},
			}}}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(reportPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			vitest := NewVitest(RunnerConfig{TestCommand: "true {{resultPath}}", ResultPath: reportPath})
			result := NewRunResult(nil)
			require.ErrorContains(t, vitest.Run(result, []plan.TestCase{{Path: "multi.spec.js"}}, false), "Vitest finished with failed tests")
			failed := result.FailedTests()
			want := []plan.TestCase{{Scope: tc.scope, Name: tc.title, Path: "multi.spec.js"}}
			if diff := cmp.Diff(want, failed); diff != "" {
				t.Fatalf("collector identities changed (-want +got):\n%s", diff)
			}
			_, args, err := vitest.CommandNameAndArgs(failed, true)
			if err != nil {
				t.Fatal(err)
			}
			pattern := regexp.MustCompile(args[slices.Index(args, "--testNamePattern")+1])
			for _, name := range tc.matches {
				if !pattern.MatchString(name) {
					t.Errorf("pattern %q does not match %q", pattern, name)
				}
				for _, unrelated := range []string{"prefix " + name, name + " suffix"} {
					if pattern.MatchString(unrelated) {
						t.Errorf("pattern %q matches unrelated test %q", pattern, unrelated)
					}
				}
			}
			for _, name := range tc.misses {
				if pattern.MatchString(name) {
					t.Errorf("pattern %q matches unrelated test %q", pattern, name)
				}
			}
		})
	}
}

func TestVitestRun_UnreadableFirstReportPreservesCommandResult(t *testing.T) {
	for _, report := range []string{"missing", "invalid"} {
		for _, exitCode := range []int{0, 7} {
			t.Run(fmt.Sprintf("%s/exit%d", report, exitCode), func(t *testing.T) {
				reportPath := filepath.Join(t.TempDir(), "vitest.json")
				script := `rm -f "$1"`
				if report == "invalid" {
					script = `printf 'invalid JSON' > "$1"`
				}
				script += fmt.Sprintf("; exit %d", exitCode)
				vitest := NewVitest(RunnerConfig{
					TestCommand: shellquote.Join("sh", "-c", script, "sh", "{{resultPath}}"),
					ResultPath:  reportPath,
				})
				result := NewRunResult(nil)
				err := vitest.Run(result, []plan.TestCase{{Path: "multi.spec.js"}}, false)
				if exitCode == 0 {
					require.NoError(t, err)
				} else {
					var exitErr *exec.ExitError
					require.ErrorAs(t, err, &exitErr)
					require.Equal(t, exitCode, exitErr.ExitCode())
				}
				require.Empty(t, result.FailedTests())
			})
		}
	}
}

func TestVitestRun_RetryMustPassToClearFailure(t *testing.T) {
	testFile, err := filepath.Abs("multi.spec.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"skipped", "pending", "todo", "omitted", "passed"} {
		t.Run(status, func(t *testing.T) {
			reportPath := filepath.Join(t.TempDir(), "vitest.json")
			vitest := NewVitest(RunnerConfig{
				TestCommand: "true {{resultPath}}", RetryTestCommand: "true {{resultPath}} {{testNamePattern}}",
				ResultPath: reportPath,
			})
			result := NewRunResult(nil)
			testCase := plan.TestCase{Scope: "suite", Name: "fails", Path: "multi.spec.js"}
			for _, attempt := range []struct {
				status string
				retry  bool
			}{{"failed", false}, {status, true}} {
				assertions := fmt.Sprintf(`[{"status":%q,"title":"fails","ancestorTitles":["suite"]}]`, attempt.status)
				if attempt.status == "omitted" {
					assertions = "[]"
				}
				report := fmt.Sprintf(`{"testResults":[{"name":%q,"assertionResults":%s}]}`, testFile, assertions)
				if err := os.WriteFile(reportPath, []byte(report), 0600); err != nil {
					t.Fatal(err)
				}
				err := vitest.Run(result, []plan.TestCase{testCase}, attempt.retry)
				if attempt.status == "passed" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "Vitest finished with failed tests")
				}
			}
			wantStatus := RunStatusFailed
			wantFailedTests := []plan.TestCase{testCase}
			if status == "passed" {
				wantStatus = RunStatusPassed
				wantFailedTests = nil
			}
			if got := result.Status(); got != wantStatus {
				t.Errorf("retry %s: status = %s, want %s", status, got, wantStatus)
			}
			if diff := cmp.Diff(wantFailedTests, result.FailedTests()); diff != "" {
				t.Errorf("retry %s: FailedTests() diff (-want +got):\n%s", status, diff)
			}
		})
	}
}
