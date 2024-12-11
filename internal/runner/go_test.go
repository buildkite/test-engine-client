package runner

import (
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestGoRun(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand: "go test {{testExamples}} -json",
		ResultPath:  "test-results.json",
	})

	testCases := []plan.TestCase{
		{
			Path: "./passing_test.go",
			Name: "TestPassing",
		},
	}
	result := NewRunResult([]plan.TestCase{})
	err := goTest.Run(result, testCases, false)

	if err != nil {
		t.Errorf("GoTest.Run(%q) error = %v", testCases, err)
	}

	if result.Status() != RunStatusPassed {
		t.Errorf("GoTest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}
}

func TestGoRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand: "go test {{testExamples}} -json",
		ResultPath:  "test-results.json",
	})

	testCases := []plan.TestCase{
		{
			Path: "./failing_test.go",
			Name: "TestFailing",
		},
	}
	result := NewRunResult([]plan.TestCase{})
	err := goTest.Run(result, testCases, false)

	if err == nil {
		t.Error("GoTest.Run() error = nil, want error")
	}

	if result.Status() != RunStatusFailed {
		t.Errorf("GoTest.Run() RunResult.Status = %v, want %v", result.Status(), RunStatusFailed)
	}
}

func TestGoTest_CommandArgs(t *testing.T) {
	tests := []struct {
		name           string
		config         RunnerConfig
		testExamples   []string
		testNameFilter string
		wantCmd        string
		wantArgs       []string
	}{
		{
			name: "basic command",
			config: RunnerConfig{
				TestCommand: "go test {{testExamples}} -json",
			},
			testExamples: []string{"./pkg/..."},
			wantCmd:      "go",
			wantArgs:     []string{"test", "./pkg/...", "-json"},
		},
		{
			name: "with test name filter",
			config: RunnerConfig{
				RetryTestCommand: "go test {{testExamples}} -run '{{testNamePattern}}' -json",
			},
			testExamples:   []string{"./pkg/..."},
			testNameFilter: "TestFoo|TestBar",
			wantCmd:        "go",
			wantArgs:       []string{"test", "./pkg/...", "-run", "TestFoo|TestBar", "-json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goTest := NewGoTest(tt.config)
			cmd := tt.config.TestCommand
			if tt.testNameFilter != "" {
				cmd = tt.config.RetryTestCommand
			}

			gotCmd, gotArgs, err := goTest.commandNameAndArgs(cmd, tt.testExamples, tt.testNameFilter)
			if err != nil {
				t.Errorf("GoTest.commandNameAndArgs() error = %v", err)
				return
			}

			if gotCmd != tt.wantCmd {
				t.Errorf("GoTest.commandNameAndArgs() gotCmd = %v, want %v", gotCmd, tt.wantCmd)
			}

			if diff := cmp.Diff(gotArgs, tt.wantArgs); diff != "" {
				t.Errorf("GoTest.commandNameAndArgs() gotArgs diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestGoTest_GetFiles(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestFilePattern: "**/*_test.go",
	})

	got, err := goTest.GetFiles()
	if err != nil {
		t.Errorf("GoTest.GetFiles() error = %v", err)
		return
	}

	want := []string{
		"failing_test.go",
		"passing_test.go",
		"pkg1/pkg1_test.go",
		"pkg2/pkg2_test.go",
		"subtests_test.go",
		"suite_test.go",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("GoTest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestGoTest_GetExamples(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestFilePattern: "**/*_test.go",
	})

	files, err := goTest.GetFiles()
	if err != nil {
		t.Fatalf("GetFiles() error = %v", err)
	}

	got, err := goTest.GetExamples(files)
	if err != nil {
		t.Fatalf("GetExamples() error = %v", err)
	}

	want := []plan.TestCase{
		{Name: "TestFailing", Path: "failing_test.go"},
		{Name: "TestPassing", Path: "passing_test.go"},
		{Name: "TestPkg1A", Path: "pkg1/pkg1_test.go"},
		{Name: "TestPkg1B", Path: "pkg1/pkg1_test.go"},
		{Name: "TestPkg2A", Path: "pkg2/pkg2_test.go"},
		{Name: "TestPkg2B", Path: "pkg2/pkg2_test.go"},
		{Name: "TestWithSubtests", Path: "subtests_test.go"},
		{Name: "TestSuiteA", Path: "suite_test.go"},
		{Name: "TestSuiteB", Path: "suite_test.go"},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("GetExamples() diff (-got +want):\n%s", diff)
	}
}

func TestGoRun_Subtests(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand: "go test {{testExamples}} -json",
		ResultPath:  "test-results.json",
	})

	testCases := []plan.TestCase{
		{Path: "./subtests_test.go"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := goTest.Run(result, testCases, false)

	if err == nil {
		t.Error("GoTest.Run() error = nil, want error")
	}

	// Check that both subtests were recorded
	tests := result.tests
	if len(tests) != 3 {
		t.Errorf("Got %d tests, want 3", len(tests))
	}

	// Check specific subtest results
	for _, tr := range tests {
		switch tr.TestCase.Name {
		case "TestWithSubtests":
			if tr.Status != TestStatusFailed {
				t.Errorf("TestWithSubtests status = %v, want %v", tr.Status, TestStatusFailed)
			}
		case "TestWithSubtests/SubtestA":
			if tr.Status != TestStatusPassed {
				t.Errorf("SubtestA status = %v, want %v", tr.Status, TestStatusPassed)
			}
		case "TestWithSubtests/SubtestB":
			if tr.Status != TestStatusFailed {
				t.Errorf("SubtestB status = %v, want %v", tr.Status, TestStatusFailed)
			}
		default:
			t.Errorf("Unexpected test name: %s", tr.TestCase.Name)
		}
	}
}

func TestGoRun_TestSuite(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand: "go test {{testExamples}} -json",
		ResultPath:  "test-results.json",
	})

	testCases := []plan.TestCase{
		{Path: "./suite_test.go"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := goTest.Run(result, testCases, false)

	if err == nil {
		t.Error("GoTest.Run() error = nil, want error")
	}

	tests := result.tests
	if len(tests) != 2 {
		t.Errorf("Got %d tests, want 2", len(tests))
	}

	// Check suite test results
	for _, tr := range tests {
		switch tr.TestCase.Name {
		case "TestSuiteA":
			if tr.Status != TestStatusPassed {
				t.Errorf("TestSuiteA status = %v, want %v", tr.Status, TestStatusPassed)
			}
		case "TestSuiteB":
			if tr.Status != TestStatusFailed {
				t.Errorf("TestSuiteB status = %v, want %v", tr.Status, TestStatusFailed)
			}
		default:
			t.Errorf("Unexpected test name: %s", tr.TestCase.Name)
		}
	}
}

func TestGoRun_Retry(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand:      "go test {{testExamples}} -json",
		RetryTestCommand: "go test {{testExamples}} -run '{{testNamePattern}}' -json",
		ResultPath:       "test-results.json",
	})

	// First run - should fail
	testCases := []plan.TestCase{
		{Path: "./failing_test.go", Name: "TestFailing"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := goTest.Run(result, testCases, false)

	if err == nil {
		t.Error("First run: error = nil, want error")
	}

	// Retry - should still fail but use the retry command
	retryResult := NewRunResult([]plan.TestCase{})
	err = goTest.Run(retryResult, testCases, true)

	if err == nil {
		t.Error("Retry: error = nil, want error")
	}

	// Check that the retry used the correct command pattern
	found := false
	for _, tr := range retryResult.tests {
		if strings.Contains(tr.TestCase.Name, "TestFailing") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Retry: test pattern did not match expected test")
	}
}

func TestGoTest_PackageSplitting(t *testing.T) {
	changeCwd(t, "./testdata/go")

	goTest := NewGoTest(RunnerConfig{
		TestCommand: "go test {{testExamples}} -run '{{testNamePattern}}' -json",
		ResultPath:  "test-results.json",
	})

	// Get all test cases
	files, err := goTest.GetFiles()
	if err != nil {
		t.Fatalf("GetFiles() error = %v", err)
	}

	testCases, err := goTest.GetExamples(files)
	if err != nil {
		t.Fatalf("GetExamples() error = %v", err)
	}

	// Run tests from different packages
	pkg1Tests := filterTestsByPackage(testCases, "pkg1")
	pkg2Tests := filterTestsByPackage(testCases, "pkg2")

	// Run pkg1 tests
	result1 := NewRunResult([]plan.TestCase{})
	err = goTest.Run(result1, pkg1Tests, false)
	if err == nil {
		t.Error("pkg1: error = nil, want error (TestPkg1B should fail)")
	}

	// Run pkg2 tests
	result2 := NewRunResult([]plan.TestCase{})
	err = goTest.Run(result2, pkg2Tests, false)
	if err != nil {
		t.Errorf("pkg2: error = %v, want nil (all tests should pass)", err)
	}
}

func filterTestsByPackage(tests []plan.TestCase, pkg string) []plan.TestCase {
	var filtered []plan.TestCase
	for _, tc := range tests {
		if strings.Contains(tc.Path, pkg) {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}
