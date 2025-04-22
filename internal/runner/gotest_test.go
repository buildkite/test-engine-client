package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestGotestRun(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		ResultPath: getRandomXmlTempFilename(),
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
		ResultPath: getRandomXmlTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello/bad"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	assert.NoError(t, err)
	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
}

func TestGotestRun_CommandFailed(t *testing.T) {
	changeCwd(t, "./testdata/go")

	gotest := NewGoTest(RunnerConfig{
		TestCommand: "gotestsum --junitfile {{resultPath}} bluhbluh",
		ResultPath:  getRandomXmlTempFilename(),
	})
	testCases := []plan.TestCase{
		{Path: "example.com/hello"},
	}
	result := NewRunResult([]plan.TestCase{})
	err := gotest.Run(result, testCases, false)

	assert.NoError(t, err) // sadly we don't have a way to reliably differentiate test fail vs build fail (yet).
	if result.Status() != RunStatusFailed {
		t.Errorf("Gotest.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
	}
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
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Gotest.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func getRandomXmlTempFilename() string {
	tempDir, err := os.MkdirTemp("", "bktec-*")
	if err != nil {
		panic(err)
	}
	return filepath.Join(tempDir, "test-results.xml")
}
