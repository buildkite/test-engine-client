package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestGotestRun(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		ResultPath: getRandomXMLTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	assert.NoError(t, err)
	if result.Status() != RunStatusPassed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	fmt.Printf("result.tests: %v\n", result.tests)

	testResult := result.tests["example.com/hello/TestHelloWorld/example.com/hello"]
	if testResult.Path != "example.com/hello" {
		t.Errorf("TestResult.Path = %v, want %v", testResult.Path, "example.com/hello")
	}
}

func TestGotestRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		ResultPath: getRandomXMLTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/bad"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
}

func TestGotestRun_BuildFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		ResultPath: getRandomXMLTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/broken"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	// A build failure is an error outside of the tests, not a test failure.
	if result.Status() != RunStatusError {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}

	// The synthetic "TestMain" testcase that gotestsum emits for a build
	// failure must not be recorded as a failed test, otherwise it would be
	// selected for retry but silently dropped from the retry command
	// (its package path is empty).
	if failed := result.FailedTests(); len(failed) != 0 {
		t.Errorf("Gotest.Run(%q) RunResult.FailedTests() = %v, want none", testCases, failed)
	}
}

func TestGotestRun_GoJSONL(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  filepath.Join(t.TempDir(), "test-results.jsonl"),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	assert.NoError(t, err)
	if result.Status() != RunStatusPassed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	testResult := result.tests["example.com/hello/TestHelloWorld/example.com/hello"]
	if testResult.Path != "example.com/hello" {
		t.Errorf("TestResult.Path = %v, want %v", testResult.Path, "example.com/hello")
	}
}

func TestGotestRun_GotestsumGoJSONL(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "gotestsum --jsonfile={{resultPath}} {{packages}}",
		ResultPath:  filepath.Join(t.TempDir(), "test-results.jsonl"),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	assert.NoError(t, err)
	if result.Status() != RunStatusPassed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
	}

	testResult := result.tests["example.com/hello/TestHelloWorld/example.com/hello"]
	if testResult.Path != "example.com/hello" {
		t.Errorf("TestResult.Path = %v, want %v", testResult.Path, "example.com/hello")
	}
}

func TestGotestRun_GoJSONLTestFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  filepath.Join(t.TempDir(), "test-results.jsonl"),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/bad"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	failed := result.FailedTests()
	if len(failed) != 1 || failed[0].Path != "example.com/hello/bad" {
		t.Errorf("Gotest.Run(%q) RunResult.FailedTests = %v, want one failed package", testCases, failed)
	}
}

func TestGotestRun_GoJSONLBuildFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  filepath.Join(t.TempDir(), "test-results.jsonl"),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/broken"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusError {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusError)
	}

	if failed := result.FailedTests(); len(failed) != 0 {
		t.Errorf("Gotest.Run(%q) RunResult.FailedTests() = %v, want none", testCases, failed)
	}
}

func TestGotestRun_GoJSONLPackageLevelFailure(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  filepath.Join(t.TempDir(), "test-results.jsonl"),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/testmain"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)
	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}

	failed := result.FailedTests()
	if len(failed) != 1 || failed[0].Path != "example.com/hello/testmain" || failed[0].Name != "TestMain" {
		t.Errorf("Gotest.Run(%q) RunResult.FailedTests = %v, want package-level failure", testCases, failed)
	}
}

func TestGotestRun_GoJSONLEmptyResultFile(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "test-results.jsonl")
	err := os.WriteFile(resultPath, nil, 0o600)
	assert.NoError(t, err)

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  resultPath,
	})
	result := NewRunResult([]plan.TestCase{})
	err = gotest.parseGoJSONLResults(result)

	if err == nil || !strings.Contains(err.Error(), "no events found") {
		t.Errorf("GoTest.parseGoJSONLResults() error = %v, want no events found", err)
	}
}

func TestGotestRun_GoJSONLFailureIsNotOverwrittenByLaterPass(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "test-results.jsonl")
	err := os.WriteFile(resultPath, []byte(`{"Action":"start","Package":"example.com/hello"}
{"Action":"fail","Package":"example.com/hello","Test":"TestFlaky"}
{"Action":"pass","Package":"example.com/hello","Test":"TestFlaky"}
{"Action":"fail","Package":"example.com/hello"}
`), 0o600)
	assert.NoError(t, err)

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "go test -json {{packages}}",
		ResultPath:  resultPath,
	})
	result := NewRunResult([]plan.TestCase{})
	err = gotest.parseGoJSONLResults(result)

	assert.NoError(t, err)
	if result.Status() != RunStatusFailed {
		t.Errorf("GoTest.parseGoJSONLResults() RunResult.Status = %v, want %v", result.Status(), RunStatusFailed)
	}

	failed := result.FailedTests()
	if len(failed) != 1 || failed[0].Name != "TestFlaky" {
		t.Errorf("GoTest.parseGoJSONLResults() RunResult.FailedTests = %v, want TestFlaky failure", failed)
	}
}

func TestGotestRun_CommandFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "gotestsum --junitfile {{resultPath}} bluhbluh",
		ResultPath:  getRandomXMLTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	exitError := new(exec.ExitError)
	assert.ErrorAs(t, err, &exitError)

	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
}

func TestGotestUploadResult_DefaultsToJUnit(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{ResultPath: "junit.xml"})

	assert.Equal(t, "junit", gotest.ResultFormat())
	assert.Equal(t, "junit.xml", gotest.ResultFilePath())
}

func TestGotestUploadResult_DetectsGoJSONLFromGoTestJSONCommand(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "go-test.jsonl",
		TestCommand: "go test -json {{packages}}",
	})

	assert.Equal(t, "go-jsonl", gotest.ResultFormat())
	assert.Equal(t, "go-test.jsonl", gotest.ResultFilePath())
}

func TestGotestUploadResult_DetectsGoJSONLFromGotestsumJSONFileCommand(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "go-test.jsonl",
		TestCommand: "gotestsum --jsonfile={{resultPath}} {{packages}}",
	})

	assert.Equal(t, "go-jsonl", gotest.ResultFormat())
	assert.Equal(t, "go-test.jsonl", gotest.ResultFilePath())
}

func TestGotestUploadResult_DoesNotDetectGoJSONLFromGotestsumGoTestJSONArg(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "junit.xml",
		TestCommand: "gotestsum --junitfile={{resultPath}} -- -json {{packages}}",
	})

	assert.Equal(t, "junit", gotest.ResultFormat())
	assert.Equal(t, "junit.xml", gotest.ResultFilePath())
}

func TestGotestCommandKeepsGotestsumJSONFileCommand(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "go-test.jsonl",
		TestCommand: "gotestsum --jsonfile={{resultPath}} -- -count=1 {{packages}}",
	})

	cmd, args, err := gotest.CommandNameAndArgs([]plan.TestCase{{Path: "example.com/hello"}}, false)

	assert.NoError(t, err)
	assert.Equal(t, "gotestsum", cmd)
	assert.Equal(t, []string{
		"--jsonfile=go-test.jsonl",
		"--",
		"-count=1",
		"example.com/hello",
	}, args)
}

func TestGotestCommandKeepsGoTestJSONCommand(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "go-test.jsonl",
		TestCommand: "go test -json -count=1 {{packages}}",
	})

	cmd, args, err := gotest.CommandNameAndArgs([]plan.TestCase{{Path: "example.com/hello"}}, false)

	assert.NoError(t, err)
	assert.Equal(t, "go", cmd)
	assert.Equal(t, []string{
		"test",
		"-json",
		"-count=1",
		"example.com/hello",
	}, args)
}

func TestGotestCommandKeepsGotestsumJUnitCommand(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		ResultPath:  "junit.xml",
		TestCommand: "gotestsum --junitfile={{resultPath}} -- -count=1 {{packages}}",
	})

	cmd, args, err := gotest.CommandNameAndArgs([]plan.TestCase{{Path: "example.com/hello"}}, false)

	assert.NoError(t, err)
	assert.Equal(t, "gotestsum", cmd)
	assert.Equal(t, []string{
		"--junitfile=junit.xml",
		"--",
		"-count=1",
		"example.com/hello",
	}, args)
}

func TestGotestGetSelectors(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{})

	got, err := gotest.GetSelectors()
	if err != nil {
		t.Errorf("Gotest.GetSelectors() error = %v", err)
	}

	// example.com/hello/notest has source files but no tests, so it must be
	// excluded from the discovered packages.
	want := []string{
		"example.com/hello",
		"example.com/hello/bad",
		"example.com/hello/broken",
		"example.com/hello/testmain",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Gotest.GetSelectors() diff (-got +want):\n%s", diff)
	}
}

func TestGotestGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{})

	// When selector based splitting is disabled, GetFiles falls back to the same
	// package discovery as GetSelectors because Go has no file-level concept.
	got, err := gotest.GetFiles()
	if err != nil {
		t.Errorf("Gotest.GetFiles() error = %v", err)
	}

	// example.com/hello/notest has source files but no tests, so it must be
	// excluded from the discovered packages.
	want := []string{
		"example.com/hello",
		"example.com/hello/bad",
		"example.com/hello/broken",
		"example.com/hello/testmain",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Gotest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestGotestCommandNameAndArgs_SelectorTasks(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		TestCommand: "gotestsum --junitfile={{resultPath}} {{packages}}",
		ResultPath:  "test-results.xml",
	})

	testCases := []plan.TestCase{
		{
			Format: plan.TestCaseFormatSelector,
			Value:  "example.com/hello",
		},
		{
			Format: plan.TestCaseFormatSelector,
			Value:  "example.com/hello/bad",
		},
		{
			Format: plan.TestCaseFormatSelector,
			Value:  "example.com/hello",
		},
	}

	gotName, gotArgs, err := gotest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Fatalf("GoTest.CommandNameAndArgs() error = %v", err)
	}

	if gotName != "gotestsum" {
		t.Errorf("GoTest.CommandNameAndArgs() name = %q, want %q", gotName, "gotestsum")
	}

	wantArgs := []string{
		"--junitfile=test-results.xml",
		"example.com/hello",
		"example.com/hello/bad",
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("GoTest.CommandNameAndArgs() args diff (-got +want):\n%s", diff)
	}
}

func TestGotestCommandNameAndArgs_PathTasksStillWork(t *testing.T) {
	gotest := NewGoTest(RunnerConfig{
		TestCommand: "gotestsum --junitfile={{resultPath}} {{packages}}",
		ResultPath:  "test-results.xml",
	})

	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
		{Path: "example.com/hello/bad"},
	}

	gotName, gotArgs, err := gotest.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Fatalf("GoTest.CommandNameAndArgs() error = %v", err)
	}

	if gotName != "gotestsum" {
		t.Errorf("GoTest.CommandNameAndArgs() name = %q, want %q", gotName, "gotestsum")
	}

	wantArgs := []string{
		"--junitfile=test-results.xml",
		"example.com/hello",
		"example.com/hello/bad",
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("GoTest.CommandNameAndArgs() args diff (-got +want):\n%s", diff)
	}
}

func getRandomXMLTempFilename() string {
	tempDir, err := os.MkdirTemp("", "bktec-*")
	if err != nil {
		panic(err)
	}
	return filepath.Join(tempDir, "test-results.xml")
}
