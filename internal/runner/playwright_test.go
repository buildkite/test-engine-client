package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPlaywrightRun(t *testing.T) {
	mockCwd(t, "./testdata/playwright")

	playwright := NewPlaywright(RunnerConfig{
		TestCommand: "yarn run playwright test",
		ResultPath:  "playwright.json",
	})

	files := []string{"./testdata/playwright/tests/example.spec.js"}
	got, err := playwright.Run(files, false)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Playwright.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Playwright.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestPlaywrightRun_TestFailed(t *testing.T) {
	mockCwd(t, "./testdata/playwright")

	playwright := NewPlaywright(RunnerConfig{
		ResultPath: "test-results/results.json",
	})

	files := []string{"./tests/failed.spec.js"}
	got, err := playwright.Run(files, false)

	want := RunResult{
		Status:      RunStatusFailed,
		FailedTests: []string{"failed.spec.js:3"},
	}

	if err != nil {
		t.Errorf("Playwright.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Playwright.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestPlaywrightCommandNameAndArgs_WithPlaceholder(t *testing.T) {
	testCases := []string{"tests/example.spec.js", "tests/failed.spec.js"}
	testCommand := "npx playwright test {{testExamples}}"

	rspec := Rspec{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

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

	rspec := Rspec{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

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

func TestCypressGetFiles(t *testing.T) {
	mockCwd(t, "./testdata/playwright")
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
