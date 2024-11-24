package runner

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestPlaywrightRun(t *testing.T) {
	changeCwd(t, "./testdata/playwright")

	playwright := NewPlaywright(RunnerConfig{
		TestCommand: "yarn run playwright test",
		ResultPath:  "test-results/results.json",
	})

	testCases := []plan.TestCase{
		{Path: "./testdata/playwright/tests/example.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := playwright.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Playwright.Run(%q) error = %v", testCases, err)
	}

	if len(result.tests) != 4 {
		t.Errorf("Rspec.Run(%q) len(RunResult.tests) = %d, want 4", testCases, len(result.tests))
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Playwright.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestPlaywrightRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/playwright")

	playwright := NewPlaywright(RunnerConfig{
		ResultPath: "test-results/results.json",
	})

	t.Cleanup(func() {
		os.Remove(playwright.ResultPath)
	})

	testCases := []plan.TestCase{
		{Path: "./tests/failed.spec.js"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := playwright.Run(result, testCases, false)

	wantFailedTests := []plan.TestCase{
		{
			Scope: " chromium failed.spec.js test group failed",
			Path:  "failed.spec.js:5",
			Name:  "failed",
		},
		{
			Scope: " firefox failed.spec.js test group failed",
			Path:  "failed.spec.js:5",
			Name:  "failed",
		},
	}

	if err != nil {
		t.Errorf("Playwright.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Playwright.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	// Sort the failed tests by scope and name when comparing
	sorter := cmp.Transformer("Sort", func(in []plan.TestCase) []plan.TestCase {
		out := append([]plan.TestCase(nil), in...) // Copy input to avoid mutating it
		slices.SortFunc(out, func(a, b plan.TestCase) int {
			return strings.Compare(a.Scope+"/"+a.Name, b.Scope+"/"+b.Name)
		})
		return out
	})

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests, sorter); diff != "" {
		t.Errorf("Playwright.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestPlaywrightCommandNameAndArgs_WithPlaceholder(t *testing.T) {
	testCases := []string{"tests/example.spec.js", "tests/failed.spec.js"}
	testCommand := "npx playwright test {{testExamples}}"

	rspec := NewPlaywright(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "npx"
	wantArgs := []string{"playwright", "test", "tests/example.spec.js", "tests/failed.spec.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPlaywrightCommandNameAndArgs_WithoutPlaceholder(t *testing.T) {
	testCases := []string{"tests/example.spec.js", "tests/failed.spec.js"}
	testCommand := "npx playwright test"

	rspec := NewPlaywright(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := rspec.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "npx"
	wantArgs := []string{"playwright", "test", "tests/example.spec.js", "tests/failed.spec.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPlaywrightGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/playwright")
	playwright := NewPlaywright(RunnerConfig{})

	got, err := playwright.GetFiles()
	if err != nil {
		t.Errorf("Playwright.GetFiles() error = %v", err)
	}

	want := []string{
		"tests/example.spec.js",
		"tests/failed.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Playwright.GetFiles() diff (-got +want):\n%s", diff)
	}
}
