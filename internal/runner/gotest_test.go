package runner

import (
	"encoding/json"
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

func TestGotestPrepareUploadResult_EmitSelectorTags(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "junit.xml")
	err := os.WriteFile(resultFile, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="1" failures="0" errors="0" time="0.123000">
	<testsuite tests="1" failures="0" time="0.123000" name="github.com/buildkite/test-engine-client/v2/internal/runner" timestamp="2026-06-24T08:58:51+10:00">
		<properties>
			<property name="go.version" value="go1.25.10 darwin/arm64"></property>
		</properties>
		<testcase classname="github.com/buildkite/test-engine-client/v2/internal/runner" name="TestGotest" time="0.123000"></testcase>
	</testsuite>
</testsuites>`), 0600)
	assert.NoError(t, err)

	gotest := NewGoTest(RunnerConfig{
		ResultPath:       resultFile,
		EmitSelectorTags: true,
	})

	uploadResult, err := gotest.PrepareUploadResult()
	if !assert.NoError(t, err) {
		return
	}
	defer uploadResult.Cleanup()

	assert.Equal(t, "json", uploadResult.Format)
	assert.NotEqual(t, resultFile, uploadResult.Path)

	data, err := os.ReadFile(uploadResult.Path)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"start_at":`)

	var tests []TestEngineTest
	assert.NoError(t, json.Unmarshal(data, &tests))

	if assert.Len(t, tests, 1) {
		assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/runner", tests[0].Tags["test.selector.primary"])
		assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/runner", tests[0].Scope)
		assert.Equal(t, "TestGotest", tests[0].Name)
		assert.Equal(t, TestStatusPassed, tests[0].Result)
		if assert.NotNil(t, tests[0].History) {
			assert.Equal(t, 0.0, tests[0].History.StartAt)
			assert.Equal(t, 0.123, tests[0].History.EndAt)
			assert.Equal(t, 0.123, tests[0].History.Duration)
		}
	}
}

func TestGotestPrepareUploadResult_OnlyBuildFailures(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "junit.xml")
	err := os.WriteFile(resultFile, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="1" failures="1" errors="1" time="0.000000">
	<testsuite tests="1" failures="1" time="0.000000" name="github.com/buildkite/test-engine-client/v2/internal/runner/testdata/go/broken" timestamp="2026-06-24T08:58:51+10:00">
		<testcase classname="" name="TestMain" time="0.000000">
			<failure message="Failed" type="">FAIL	github.com/buildkite/test-engine-client/v2/internal/runner/testdata/go/broken [build failed]</failure>
		</testcase>
	</testsuite>
</testsuites>`), 0600)
	assert.NoError(t, err)

	gotest := NewGoTest(RunnerConfig{
		ResultPath:       resultFile,
		EmitSelectorTags: true,
	})

	uploadResult, err := gotest.PrepareUploadResult()
	assert.NoError(t, err)
	assert.Equal(t, "junit", uploadResult.Format)
	assert.Equal(t, resultFile, uploadResult.Path)
	assert.Nil(t, uploadResult.Cleanup)
}

func TestGoTestSelectorTags(t *testing.T) {
	test := JUnitXMLTestCase{
		Classname: "github.com/buildkite/test-engine-client/v2/internal/runner",
		Name:      "TestGotest",
	}

	assert.Equal(t, map[string]string{goTestSelectorTagKey: "github.com/buildkite/test-engine-client/v2/internal/runner"}, goTestSelectorTags(test))
	assert.Equal(t, map[string]string{goTestSelectorTagKey: strings.Repeat("a", 127)}, goTestSelectorTags(JUnitXMLTestCase{Classname: strings.Repeat("a", 127)}))
	assert.Nil(t, goTestSelectorTags(JUnitXMLTestCase{Name: "TestMain"}))
	assert.Nil(t, goTestSelectorTags(JUnitXMLTestCase{Classname: strings.Repeat("a", 128)}))
}

func TestGotestGetFiles(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{})

	got, err := gotest.GetFiles()
	if err != nil {
		t.Errorf("Gotest.GetFiles() error = %v", err)
	}

	want := []string{
		"example.com/hello",
		"example.com/hello/bad",
		"example.com/hello/broken",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Gotest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func getRandomXMLTempFilename() string {
	tempDir, err := os.MkdirTemp("", "bktec-*")
	if err != nil {
		panic(err)
	}
	return filepath.Join(tempDir, "test-results.xml")
}
