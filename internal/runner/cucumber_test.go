package runner

import (
    "errors"
    "os"
    "os/exec"
    "syscall"
    "testing"

    "github.com/buildkite/test-engine-client/internal/plan"
    "github.com/google/go-cmp/cmp"
    "github.com/kballard/go-shellquote"
)

func TestNewCucumber(t *testing.T) {
    cases := []struct {
        input RunnerConfig
        want  RunnerConfig
    }{
        // default
        {
            input: RunnerConfig{},
            want: RunnerConfig{
                TestCommand:            "bundle exec cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}",
                TestFilePattern:        "features/**/*.feature",
                TestFileExcludePattern: "",
                RetryTestCommand:       "bundle exec cucumber --format pretty --format json --out {{resultPath}} {{testExamples}}",
            },
        },
        // custom
        {
            input: RunnerConfig{
                TestCommand:            "cucumber --format json --out {{resultPath}} {{testExamples}}",
                TestFilePattern:        "features/api/**/*.feature",
                TestFileExcludePattern: "features/experimental",
                RetryTestCommand:       "cucumber --format json --out {{resultPath}} {{testExamples}}",
            },
            want: RunnerConfig{
                TestCommand:            "cucumber --format json --out {{resultPath}} {{testExamples}}",
                TestFilePattern:        "features/api/**/*.feature",
                TestFileExcludePattern: "features/experimental",
                RetryTestCommand:       "cucumber --format json --out {{resultPath}} {{testExamples}}",
            },
        },
    }

    for _, c := range cases {
        got := NewCucumber(c.input)
        if diff := cmp.Diff(got.RunnerConfig, c.want); diff != "" {
            t.Errorf("NewCucumber(%v) diff (-got +want):\n%s", c.input, diff)
        }
    }
}

func TestCucumberRun(t *testing.T) {
    changeCwd(t, "./testdata/cucumber")

    cucumber := NewCucumber(RunnerConfig{
        TestCommand: "bundle exec cucumber --format json --out {{resultPath}}",
        ResultPath:  "tmp/cucumber.json",
    })

    t.Cleanup(func() {
        os.Remove(cucumber.ResultPath)
    })

    testCases := []plan.TestCase{
        {Path: "./features/spells/expelliarmus.feature"},
    }
    result := NewRunResult([]plan.TestCase{})
    err := cucumber.Run(result, testCases, false)

    if err != nil {
        t.Errorf("Cucumber.Run(%q) error = %v", testCases, err)
    }

    if result.Status() != RunStatusPassed {
        t.Errorf("Cucumber.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusPassed)
    }
}

func TestCucumberRun_TestFailed(t *testing.T) {
    changeCwd(t, "./testdata/cucumber")

    cucumber := NewCucumber(RunnerConfig{
        TestCommand: "bundle exec cucumber --format json --out {{resultPath}}",
        ResultPath:  "tmp/cucumber.json",
    })

    t.Cleanup(func() {
        os.Remove(cucumber.ResultPath)
    })

    testCases := []plan.TestCase{
        {Path: "./features/failure.feature"},
    }
    result := NewRunResult([]plan.TestCase{})
    err := cucumber.Run(result, testCases, false)

    if err != nil {
        t.Errorf("Cucumber.Run(%q) error = %v", testCases, err)
    }

    if result.Status() != RunStatusFailed {
        t.Errorf("Cucumber.Run(%q) RunResult.Status = %v, want %v", testCases, result.Status(), RunStatusFailed)
    }

    if len(result.FailedTests()) == 0 {
        t.Errorf("Cucumber.Run(%q) expected failed tests but got none", testCases)
    }
}

func TestCucumberGetFiles(t *testing.T) {
    cucumber := NewCucumber(RunnerConfig{})

    got, err := cucumber.GetFiles()
    if err != nil {
        t.Errorf("Cucumber.GetFiles() error = %v", err)
    }

    want := []string{
        "testdata/cucumber/features/failure.feature",
        "testdata/cucumber/features/spells/expelliarmus.feature",
    }

    if diff := cmp.Diff(got, want); diff != "" {
        t.Errorf("Cucumber.GetFiles() diff (-got +want):\n%s", diff)
    }
}

func TestCucumberCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
    testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
    testCommand := "cucumber --format json --out {{resultPath}} {{testExamples}}"

    c := NewCucumber(RunnerConfig{
        TestCommand: testCommand,
        ResultPath:  "cucumber.json",
    })

    gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)
    if err != nil {
        t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
    }

    wantName := "cucumber"
    wantArgs := []string{"--format", "json", "--out", "cucumber.json", "features/spells/expelliarmus.feature", "features/failure.feature"}

    if diff := cmp.Diff(gotName, wantName); diff != "" {
        t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
    }
    if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
        t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
    }
}

func TestCucumberCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
    testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
    testCommand := "cucumber --format json --out {{resultPath}}"

    c := NewCucumber(RunnerConfig{
        TestCommand: testCommand,
        ResultPath:  "cucumber.json",
    })

    gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)
    if err != nil {
        t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
    }

    wantName := "cucumber"
    wantArgs := []string{"--format", "json", "--out", "cucumber.json", "features/spells/expelliarmus.feature", "features/failure.feature"}

    if diff := cmp.Diff(gotName, wantName); diff != "" {
        t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
    }
    if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
        t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
    }
}

func TestCucumberCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
    testCases := []string{"features/spells/expelliarmus.feature", "features/failure.feature"}
    testCommand := "cucumber --format json --out '{{resultPath}} {{testExamples}}"

    c := NewCucumber(RunnerConfig{
        TestCommand: testCommand,
    })

    gotName, gotArgs, err := c.commandNameAndArgs(testCommand, testCases)

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
