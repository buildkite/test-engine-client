package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
	"github.com/stretchr/testify/assert"
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

	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand:      "pytest failed_test.py",
			RetryTestCommand: "pytest",
			ResultPath:       "result-passed.json",
		},
	}

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

	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "pytest",
			ResultPath:  "result-failed.json",
		},
	}
	testCases := []plan.TestCase{
		{Path: "failed_test.py"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

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

func TestPytestRun_CollectionError(t *testing.T) {
	if !checkPythonPackageInstalled("pytest") {
		t.Skip("pytest Python package is not installed")
	}
	if !checkPythonPackageInstalled("buildkite_test_collector") {
		t.Skip("buildkite-test-collector Python package is not installed")
	}

	changeCwd(t, "./testdata/pytest_collection_error")

	resultPath := filepath.Join(t.TempDir(), "result.json")
	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "python -m pytest {{testExamples}} --json={{resultPath}}",
			ResultPath:  resultPath,
		},
	}
	testCases := []plan.TestCase{{Path: "test_broken_import.py"}}
	result := NewRunResult([]plan.TestCase{})

	err := pytest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	if !assert.ErrorAs(t, err, &exitError) {
		return
	}
	if exitError.ExitCode() != 2 {
		t.Errorf("Pytest.Run(%q) exit code = %d, want 2", testCases, exitError.ExitCode())
	}

	if result.Status() != RunStatusError {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}
	if result.Error() == nil {
		t.Fatalf("Pytest.Run(%q) RunResult.Error = nil, want error", testCases)
	}
	if got, want := result.Error().Error(), "pytest collection failed: test_broken_import.py"; got != want {
		t.Errorf("Pytest.Run(%q) RunResult.Error = %q, want %q", testCases, got, want)
	}

	failedTests := result.FailedTests()
	if len(failedTests) != 1 {
		t.Fatalf("len(result.FailedTests()) = %d, want 1", len(failedTests))
	}
	failedTest := failedTests[0]
	if failedTest.Scope != "" {
		t.Errorf("Pytest.Run(%q) failed test scope = %q, want empty", testCases, failedTest.Scope)
	}
	if failedTest.Name != "test_broken_import.py" {
		t.Errorf("Pytest.Run(%q) failed test name = %q, want %q", testCases, failedTest.Name, "test_broken_import.py")
	}
	if failedTest.Path != "test_broken_import.py" {
		t.Errorf("Pytest.Run(%q) failed test path = %q, want %q", testCases, failedTest.Path, "test_broken_import.py")
	}
}

func TestPytestRun_JSONExit2ParsesCollectionError(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "result.json")
	json := `[
		{
			"id": "collection-error-id",
			"scope": "",
			"name": "tests/test_broken.py",
			"result": "failed",
			"tags": {"test.pytest_collection_error": "true"}
		}
	]`
	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "sh -c 'printf %s \"$1\" > \"$2\"; exit 2' sh " + shellquote.Join(json) + " {{resultPath}}",
			ResultPath:  resultPath,
		},
	}
	result := NewRunResult([]plan.TestCase{})

	err := pytest.Run(result, nil, false)

	exitError := new(exec.ExitError)
	if !assert.ErrorAs(t, err, &exitError) {
		return
	}
	if exitError.ExitCode() != 2 {
		t.Errorf("Pytest.Run() exit code = %d, want 2", exitError.ExitCode())
	}
	if result.Status() != RunStatusError {
		t.Errorf("Pytest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusError)
	}
	if result.Error() == nil {
		t.Fatal("Pytest.Run() RunResult.Error = nil, want error")
	}
	if got, want := result.Error().Error(), "pytest collection failed: tests/test_broken.py"; got != want {
		t.Errorf("Pytest.Run() RunResult.Error = %q, want %q", got, want)
	}
}

func TestPytestRun_JSONExit2WithoutCollectionErrorIsTerminal(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "result.json")
	json := `[
		{
			"id": "failed-test-id",
			"scope": "tests/test_sample.py",
			"name": "test_failed",
			"result": "failed"
		}
	]`
	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "sh -c 'printf %s \"$1\" > \"$2\"; exit 2' sh " + shellquote.Join(json) + " {{resultPath}}",
			ResultPath:  resultPath,
		},
	}
	result := NewRunResult([]plan.TestCase{})

	err := pytest.Run(result, nil, false)

	exitError := new(exec.ExitError)
	if !assert.ErrorAs(t, err, &exitError) {
		return
	}
	if exitError.ExitCode() != 2 {
		t.Errorf("Pytest.Run() exit code = %d, want 2", exitError.ExitCode())
	}
	if result.Status() != RunStatusError {
		t.Errorf("Pytest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusError)
	}
	resultExitError := new(exec.ExitError)
	if !assert.ErrorAs(t, result.Error(), &resultExitError) {
		return
	}
	if resultExitError.ExitCode() != 2 {
		t.Errorf("Pytest.Run() RunResult.Error exit code = %d, want 2", resultExitError.ExitCode())
	}
}

func TestPytestRunParseJSON_CollectionError(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "result.json")
	json := `[
		{
			"id": "collection-error-id",
			"scope": "",
			"name": "tests/test_broken.py",
			"file_name": "tests/test_broken.py",
			"location": "tests/test_broken.py",
			"result": "failed",
			"tags": {"test.pytest_collection_error": "true"}
		}
	]`
	if err := os.WriteFile(resultPath, []byte(json), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", resultPath, err)
	}

	pytest := Pytest{RunnerConfig: RunnerConfig{ResultPath: resultPath}}
	result := NewRunResult([]plan.TestCase{})

	if err := pytest.runParseJSON(result); err != nil {
		t.Fatalf("Pytest.runParseJSON() error = %v", err)
	}

	if result.Status() != RunStatusError {
		t.Errorf("Pytest.runParseJSON() RunResult.Status = %v, want %v", result.Status(), RunStatusError)
	}
	if result.Error() == nil {
		t.Fatal("Pytest.runParseJSON() RunResult.Error = nil, want error")
	}
	if got, want := result.Error().Error(), "pytest collection failed: tests/test_broken.py"; got != want {
		t.Errorf("Pytest.runParseJSON() RunResult.Error = %q, want %q", got, want)
	}

	wantFailedTests := []plan.TestCase{
		{
			Identifier: "collection-error-id",
			Format:     plan.TestCaseFormatExample,
			Scope:      "",
			Name:       "tests/test_broken.py",
			Path:       "tests/test_broken.py",
		},
	}

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Pytest.runParseJSON() RunResult.FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestPytestRunParseJSON_EmptyScopeWithoutCollectionErrorTag(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "result.json")
	json := `[
		{
			"id": "empty-scope-id",
			"scope": "",
			"name": "tests/test_file.py",
			"result": "failed"
		}
	]`
	if err := os.WriteFile(resultPath, []byte(json), 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", resultPath, err)
	}

	pytest := Pytest{RunnerConfig: RunnerConfig{ResultPath: resultPath}}
	result := NewRunResult([]plan.TestCase{})

	if err := pytest.runParseJSON(result); err != nil {
		t.Fatalf("Pytest.runParseJSON() error = %v", err)
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("Pytest.runParseJSON() RunResult.Status = %v, want %v", result.Status(), RunStatusFailed)
	}

	wantFailedTests := []plan.TestCase{
		{
			Identifier: "empty-scope-id",
			Format:     plan.TestCaseFormatExample,
			Scope:      "",
			Name:       "tests/test_file.py",
			Path:       "tests/test_file.py",
		},
	}

	if diff := cmp.Diff(result.FailedTests(), wantFailedTests); diff != "" {
		t.Errorf("Pytest.runParseJSON() RunResult.FailedTests() diff (-got +want):\n%s", diff)
	}
}

func TestPytestPathFromTestEngineResult(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		testName string
		wantPath string
	}{
		{
			name:     "scoped test",
			scope:    "tests/test_sample.py",
			testName: "test_happy",
			wantPath: "tests/test_sample.py::test_happy",
		},
		{
			name:     "collection error",
			scope:    "",
			testName: "tests/test_broken.py",
			wantPath: "tests/test_broken.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pytestPathFromTestEngineResult(tt.scope, tt.testName)
			if got != tt.wantPath {
				t.Errorf("pytestPathFromTestEngineResult(%q, %q) = %q, want %q", tt.scope, tt.testName, got, tt.wantPath)
			}
		})
	}
}

func TestPytestRun_JUnit_TestPassed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "pytest",
			ResultPath:  "result-passed.xml",
		},
		useJUnit: true,
	}
	testCases := []plan.TestCase{{Path: "test_sample.py"}}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)
	if err != nil {
		t.Errorf("Pytest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("Pytest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	var passedTests []plan.TestCase
	for _, tr := range result.tests {
		if tr.Status == TestStatusPassed {
			passedTests = append(passedTests, tr.TestCase)
		}
	}
	wantPassedTests := []plan.TestCase{
		{
			Format:     "example",
			Identifier: "test_sample.py::test_happy",
			Name:       "test_happy",
			Path:       "test_sample.py::test_happy",
			Scope:      "test_sample",
		},
	}
	if diff := cmp.Diff(passedTests, wantPassedTests); diff != "" {
		t.Errorf("Pytest.Run(%q) passed tests diff (-got +want):\n%s", testCases, diff)
	}
}

func TestPytestRun_JUnit_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: "pytest",
			ResultPath:  "result-failed.xml",
		},
		useJUnit: true,
	}
	testCases := []plan.TestCase{{Path: "failed_test.py"}}
	result := NewRunResult([]plan.TestCase{})
	err := pytest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

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
			Identifier: "tests/failed_test.py::test_failed",
			Name:       "test_failed",
			Path:       "tests/failed_test.py::test_failed",
			Scope:      "tests.failed_test",
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
	assert.ErrorAs(t, err, &exitError)
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
	assert.ErrorAs(t, err, &exitError)
}

func TestPytestDiscoverTestTargets(t *testing.T) {
	changeCwd(t, "./testdata/pytest")

	pytest := NewPytest(RunnerConfig{})

	got, err := pytest.DiscoverTestTargets()
	if err != nil {
		t.Errorf("Pytest.DiscoverTestTargets() error = %v", err)
	}

	want := []string{
		"failed_test.py",
		"spells/test_expelliarmus.py",
		"test_sample.py",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Pytest.DiscoverTestTargets() diff (-got +want):\n%s", diff)
	}
}

func TestPytestCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []plan.TestCase{{Path: "failed_test.py"}, {Path: "test_sample.py"}}
	testCommand := "pytest {{testExamples}} --full-trace --json={{resultPath}}"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
		ResultPath:  "result.json",
	})

	gotName, gotArgs, err := pytest.CommandNameAndArgs(testCases, false)
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
	testCases := []plan.TestCase{{Path: "failed_test.py"}, {Path: "test_sample.py"}}
	testCommand := "pytest --full-trace"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.CommandNameAndArgs(testCases, false)
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
	testCases := []plan.TestCase{{Path: "failed_test.py"}, {Path: "test_sample.py"}}
	testCommand := "pytest '{{testExamples}}"

	pytest := NewPytest(RunnerConfig{
		TestCommand: testCommand,
	})

	gotName, gotArgs, err := pytest.CommandNameAndArgs(testCases, false)

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
	testCases := []plan.TestCase{
		{Path: "foo/bar.py::TestFoo::test_foo[min-WeightedScalar-valid_reduce_ops0-only sum or avg are supported-2]"},
		{Path: "test_sample.py::test_simple"},
	}
	testCommand := "pytest {{testExamples}} --json={{resultPath}}"

	// Create Pytest struct directly to avoid NewPytest's Python package check
	pytest := Pytest{
		RunnerConfig: RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "result.json",
		},
	}

	gotName, gotArgs, err := pytest.CommandNameAndArgs(testCases, false)
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
			TagFilters: "team:frontend",
		},
	)

	files, _ := pytest.DiscoverTestTargets()

	got, err := pytest.GetExamples(files)
	if err != nil {
		t.Fatalf("Pytest.GetExamples(%q) error = %v", files, err)
	}

	if len(got) != 2 {
		t.Fatalf("Pytest.GetExamples(%q) with tag filter 'team:frontend' returned %d tests, want 2", files, len(got))
	}

	if got[0].Name != "test_knocks_wand_out" {
		t.Errorf("got[0].Name = %q, want %q", got[0].Name, "test_knocks_wand_out")
	}

	if got[1].Name != "test_happy" {
		t.Errorf("got[0].Name = %q, want %q", got[0].Name, "test_happy")
	}
}

func TestPytestNodeIDFromJUnit(t *testing.T) {
	tests := []struct {
		classname string
		name      string
		wantScope string
		wantPath  string
	}{
		{
			classname: "test_sample",
			name:      "test_happy",
			wantPath:  "test_sample.py::test_happy",
		},
		{
			classname: "tests.test_sample",
			name:      "test_happy",
			wantPath:  "tests/test_sample.py::test_happy",
		},
		{
			classname: "tests.failed_test",
			name:      "test_failed",
			wantPath:  "tests/failed_test.py::test_failed",
		},
		{
			classname: "test_auth.TestLogin",
			name:      "test_success",
			wantPath:  "test_auth.py::TestLogin::test_success",
		},
		{
			classname: "tests.test_auth.TestLogin",
			name:      "test_success",
			wantPath:  "tests/test_auth.py::TestLogin::test_success",
		},
		{
			classname: "",
			name:      "test_something",
			wantPath:  "test_something",
		},
		{
			// Uppercase package directory: MyTest is a directory, TestSubtract is a class.
			classname: "tests.MyTest.test_subtract.TestSubtract",
			name:      "test_positive",
			wantScope: "tests/MyTest/test_subtract.py::TestSubtract",
			wantPath:  "tests/MyTest/test_subtract.py::TestSubtract::test_positive",
		},
		{
			// Uppercase package directory with no class: MyTest is a directory.
			classname: "tests.MyTest.test_add",
			name:      "test_add",
			wantPath:  "tests/MyTest/test_add.py::test_add",
		},
	}

	for _, tt := range tests {
		t.Run(tt.classname+"::"+tt.name, func(t *testing.T) {
			gotPath := pytestNodeIDFromJUnit(tt.classname, tt.name)
			if gotPath != tt.wantPath {
				t.Errorf("pytestNodeIDFromJUnit(%q, %q) path = %q, want %q", tt.classname, tt.name, gotPath, tt.wantPath)
			}
		})
	}
}

func TestParsePytestCollectOutput(t *testing.T) {
	output := `test_sample.py::test_happy
test_auth.py::TestLogin::test_success
test_auth.py::test_param[value1]

3 tests collected in 0.05s`

	pytest := Pytest{useJUnit: false}

	got, err := pytest.parsePytestCollectOutput(output)
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

func TestParsePytestCollectOutput_JUnit(t *testing.T) {
	pytest := Pytest{useJUnit: true}
	output := `test_sample.py::test_happy
test_auth.py::TestLogin::test_success
test_auth.py::test_param[value1]
tests/test_another.py::WithClass::test_method
tests/test_nested.py::TestOuter::TestInner::test_deep
tests/pkg.py/test_auth.py::TestLogin::test_success

6 tests collected in 0.05s`
	got, err := pytest.parsePytestCollectOutput(output)
	if err != nil {
		t.Fatalf("parsePytestCollectOutput() error = %v", err)
	}

	want := []plan.TestCase{
		{Identifier: "test_sample.py::test_happy", Path: "test_sample.py::test_happy", Scope: "test_sample", Name: "test_happy", Format: plan.TestCaseFormatExample},
		{Identifier: "test_auth.py::TestLogin::test_success", Path: "test_auth.py::TestLogin::test_success", Scope: "test_auth.TestLogin", Name: "test_success", Format: plan.TestCaseFormatExample},
		{Identifier: "test_auth.py::test_param[value1]", Path: "test_auth.py::test_param[value1]", Scope: "test_auth", Name: "test_param[value1]", Format: plan.TestCaseFormatExample},
		{Identifier: "tests/test_another.py::WithClass::test_method", Path: "tests/test_another.py::WithClass::test_method", Scope: "tests.test_another.WithClass", Name: "test_method", Format: plan.TestCaseFormatExample},
		// Nested classes produce more than one `::` in the scope.
		{Identifier: "tests/test_nested.py::TestOuter::TestInner::test_deep", Path: "tests/test_nested.py::TestOuter::TestInner::test_deep", Scope: "tests.test_nested.TestOuter.TestInner", Name: "test_deep", Format: plan.TestCaseFormatExample},
		// A directory ending in `.py` must keep its extension; only the file loses it.
		{Identifier: "tests/pkg.py/test_auth.py::TestLogin::test_success", Path: "tests/pkg.py/test_auth.py::TestLogin::test_success", Scope: "tests.pkg.py.test_auth.TestLogin", Name: "test_success", Format: plan.TestCaseFormatExample},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsePytestCollectOutput() diff (-got +want):\n%s", diff)
	}
}

func TestPytestResultFormat_JUnit(t *testing.T) {
	pytest := Pytest{useJUnit: true}
	if got := pytest.ResultFormat(); got != "junit" {
		t.Errorf("ResultFormat() = %q, want %q", got, "junit")
	}
}

func TestPytestResultFormat_JSON(t *testing.T) {
	pytest := Pytest{useJUnit: false}
	if got := pytest.ResultFormat(); got != "" {
		t.Errorf("ResultFormat() = %q, want %q (empty, test collector handles upload)", got, "")
	}
}
