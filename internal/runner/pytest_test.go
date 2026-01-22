package runner

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

// Testing happy path where all configurtions are auto configured.
func TestPytestRun(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})
	testCases := []plan.TestCase{
		{Path: "test_sample.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Pytest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestPytestRun_RetryCommand(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{
		TestCommand:      "pytest failed_test.py",
		RetryTestCommand: "pytest",
	})

	testCases := []plan.TestCase{
		{Path: "test_sample.py"},
	}

	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, true)

	if err != nil {
		t.Errorf("Pytest.Run(%q) error = %v", testCases, err)
	}
}

func TestPytestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{
		TestCommand: "pytest",
		ResultPath:  "result-failed.json",
	})
	testCases := []plan.TestCase{
		{Path: "failed_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("Pytest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	failedTest := result.FailedTests()

	if len(failedTest) != 1 {
		t.Errorf("len(result.FailedTests()) = %d, want 1", len(failedTest))
	}

	wantFailedTests := []plan.TestCase{
		{
			Format:     "example",
			Identifier: "a1be7e52-0dba-4018-83ce-a1598ca68807",
			Name:       "test_failed",
			Path:       "tests/failed_test.py::test_failed",
			Scope:      "tests/failed_test.py",
		},
	}

	if diff := cmp.Diff(failedTest, wantFailedTests); diff != "" {
		t.Errorf("Pytest.Run(%q) RunResult.FailedTests() diff (-got +want):\n%s", testCases, diff)
	}
}

func TestPytestRun_TestFailedWithoutResultFile(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	// When there is TestCommand, but it didn't leave space for ResultPath
	pytest := NewPytest(RunnerConfig{
		TestCommand: "pytest",
	})
	testCases := []plan.TestCase{
		{Path: "failed_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Pytest.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestPytestRun_CommandFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{
		TestCommand: "pytest help",
	})

	testCases := []plan.TestCase{
		{Path: "test_sample.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	if result.Status() != RunStatusUnknown {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusUnknown)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Pytest.Run(%q) error type = %T (%v), want *exec.ExitError", testCases, err, err)
	}
}

func TestPytestGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})

	got, err := pytest.GetFiles()
	if err != nil {
		t.Errorf("Pytest.GetFiles() error = %v", err)
	}

	want := []string{
		"failed_test.py",
		"spells/test_expelliarmus.py",
		"test_sample.py",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Pytest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestPytestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest {{testExamples}} --full-trace --json={{resultPath}}"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "result.json",
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pytest"
	wantArgs := []string{"failed_test.py", "test_sample.py", "--full-trace", "--json=result.json"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest --full-trace"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pytest"
	wantArgs := []string{"--full-trace", "failed_test.py", "test_sample.py"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"failed_test.py", "test_sample.py"}
	testCommand := "pytest '{{testExamples}}"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)

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

func TestPytestCommandNameAndArgs_WithSpacesInTestCase(t *testing.T) {
	testCases := []string{
		"foo/bar.py::TestFoo::test_foo[min-WeightedScalar-valid_reduce_ops0-only sum or avg are supported-2]",
		"test_sample.py::test_simple",
	}
	testCommand := "pytest {{testExamples}} --json={{resultPath}}"

	// Create Pytest struct directly to avoid NewPytest's Python package check
	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "result.json",
		},
	}

	gotName, gotArgs, err := pytest.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "pytest"
	wantArgs := []string{
		"foo/bar.py::TestFoo::test_foo[min-WeightedScalar-valid_reduce_ops0-only sum or avg are supported-2]",
		"test_sample.py::test_simple",
		"--json=result.json",
	}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) name diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) args diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestPytestGetExamples(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})
	files := []string{"spells/test_expelliarmus.py"}
	got, err := pytest.GetExamples(files)

	if err != nil {
		t.Fatalf("Pytest.GetExamples(%q) error = %v", files, err)
	}

	want := []plan.TestCase{
		{
			Identifier: "spells/test_expelliarmus.py::TestExpelliarmus::test_disarms_opponent",
			Name:       "test_disarms_opponent",
			Path:       "spells/test_expelliarmus.py::TestExpelliarmus::test_disarms_opponent",
			Scope:      "spells/test_expelliarmus.py::TestExpelliarmus",
			Format:     plan.TestCaseFormatExample,
		},
		{
			Identifier: "spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
			Name:       "test_knocks_wand_out",
			Path:       "spells/test_expelliarmus.py::TestExpelliarmus::test_knocks_wand_out",
			Scope:      "spells/test_expelliarmus.py::TestExpelliarmus",
			Format:     plan.TestCaseFormatExample,
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Pytest.GetExamples(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestPytestGetExamples_EmptyFiles(t *testing.T) {
	pytest := NewPytest(RunnerConfig{})
	got, err := pytest.GetExamples([]string{})

	if err != nil {
		t.Errorf("Pytest.GetExamples([]) error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Pytest.GetExamples([]) = %v, want empty slice", got)
	}
}

func TestPytestGetExamples_TagFilter(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(
		RunnerConfig{
			TagFilters: "test_execution",
		},
	)

	files, _ := pytest.GetFiles()

	got, err := pytest.GetExamples(files)
	if err != nil {
		t.Fatalf("Pytest.GetExamples(%q) error = %v", files, err)
	}

	if len(got) != 1 {
		t.Fatalf("Pytest.GetExamples(%q) with tag filter 'test_execution' returned %d tests, want 1", files, len(got))
	}

	if got[0].Name != "test_happy" {
		t.Errorf("got[0].Name = %q, want %q", got[0].Name, "test_happy")
	}
}

func TestParsePytestCollectOutput(t *testing.T) {
	output := `test_sample.py::test_happy
test_auth.py::TestLogin::test_success
test_auth.py::test_param[value1]

3 tests collected in 0.05s`

	got, err := parsePytestCollectOutput(output)
	if err != nil {
		t.Fatalf("parsePytestCollectOutput() error = %v", err)
	}

	want := []plan.TestCase{
		{Identifier: "test_sample.py::test_happy", Path: "test_sample.py::test_happy", Scope: "test_sample.py", Name: "test_happy", Format: plan.TestCaseFormatExample},
		{Identifier: "test_auth.py::TestLogin::test_success", Path: "test_auth.py::TestLogin::test_success", Scope: "test_auth.py::TestLogin", Name: "test_success", Format: plan.TestCaseFormatExample},
		{Identifier: "test_auth.py::test_param[value1]", Path: "test_auth.py::test_param[value1]", Scope: "test_auth.py", Name: "test_param[value1]", Format: plan.TestCaseFormatExample},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsePytestCollectOutput() diff (-got +want):\n%s", diff)
	}
}
