package runner

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestCustom_NewCustom_MissingTestCommand(t *testing.T) {
	{
		_, err := NewCustom(RunnerConfig{
			TestCommand:      "",
			TestFilePattern:  "tests/**/test_*.sh",
			RetryTestCommand: "",
		})

		if err == nil || err.Error() != "test command must be provided for custom runner" {
			t.Errorf("NewCustom() error = %v, want %q", err, "test command must be provided for custom runner")
		}
	}
}

func TestCustom_NewCustom_MissingTestFilePattern(t *testing.T) {
	{
		_, err := NewCustom(RunnerConfig{
			TestCommand:      "bin/test",
			TestFilePattern:  "",
			RetryTestCommand: "",
		})

		if err == nil || err.Error() != "test file pattern must be provided for custom runner" {
			t.Errorf("NewCustom() error = %v, want %q", err, "test file pattern must be provided for custom runner")
		}
	}
}

func TestCustom_GetExamples(t *testing.T) {
	custom, err := NewCustom(RunnerConfig{
		TestCommand:     "bin/test",
		TestFilePattern: "tests/**/test_*.sh",
	})

	if err != nil {
		t.Fatalf("Failed to create Custom runner: %v", err)
	}

	_, err = custom.GetExamples([]string{"tests/test_a.sh", "tests/test_b.sh"})
	if err == nil || err.Error() != "not supported for custom runner" {
		t.Errorf("GetExamples() error = %v, want %q", err, "not supported for custom runner")
	}
}

func TestCustom_GetFiles(t *testing.T) {
	changeCwd(t, "./testdata/custom")
	custom, err := NewCustom(RunnerConfig{
		TestCommand:     "./test {{testExamples}}",
		TestFilePattern: "tests/**/test_*.sh",
	})

	if err != nil {
		t.Fatalf("Failed to create Custom runner: %v", err)
	}

	got, err := custom.GetFiles()
	if err != nil {
		t.Errorf("Custom.GetFiles() error = %v", err)
	}

	want := []string{
		"tests/test_a.sh",
		"tests/test_b.sh",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Custom.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestCustom_CommandNameAndArgs(t *testing.T) {
	custom, err := NewCustom(RunnerConfig{
		TestCommand:     "bin/test -- {{testExamples}}",
		TestFilePattern: "tests/**/test_*.sh",
	})

	if err != nil {
		t.Fatalf("Failed to create Custom runner: %v", err)
	}

	testCases := []string{"tests/test_a.sh", "tests/test_b.sh"}

	gotName, gotArgs, err := custom.commandNameAndArgs(custom.TestCommand, testCases)
	if err != nil {
		t.Errorf("Custom.cmdNameAndArgs() error = %v", err)
	}

	wantName := "bin/test"
	wantArgs := []string{"--", "tests/test_a.sh", "tests/test_b.sh"}

	if gotName != wantName {
		t.Errorf("Custom.cmdNameAndArgs() name = %v, want %v", gotName, wantName)
	}

	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("Custom.cmdNameAndArgs() args diff (-got +want):\n%s", diff)
	}
}

func TestCustom_Run(t *testing.T) {
	changeCwd(t, "./testdata/custom")
	custom, err := NewCustom(RunnerConfig{
		TestCommand:     "./test {{testExamples}}",
		TestFilePattern: "tests/**/test_*.sh",
	})

	if err != nil {
		t.Fatalf("Failed to create Custom runner: %v", err)
	}

	testCases := []plan.TestCase{
		{Path: "./tests/test_a.sh"},
		{Path: "./tests/test_b.sh"},
	}

	result := NewRunResult([]plan.TestCase{})
	err = custom.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Custom.Run(%q) error = %v", testCases, err)
	}
	if result.Status() != RunStatusUnknown {
		t.Errorf("Custom.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}
}

func TestCustom_Run_FailingTest(t *testing.T) {
	changeCwd(t, "./testdata/custom")
	custom, err := NewCustom(RunnerConfig{
		TestCommand:     "./test {{testExamples}}",
		TestFilePattern: "./tests/**/test_*.sh",
	})

	if err != nil {
		t.Fatalf("Failed to create Custom runner: %v", err)
	}

	testCases := []plan.TestCase{
		{Path: "./tests/fail_test.sh"},
	}
	result := NewRunResult([]plan.TestCase{})
	err = custom.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Custom.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Custom.Run() error type = %T (%v), want *exec.ExitError", err, err)
	}
}
