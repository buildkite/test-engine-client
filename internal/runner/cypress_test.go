package runner

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kballard/go-shellquote"
)

func TestCypressRun(t *testing.T) {
	changeCwd(t, "./testdata/cypress")

	cypress := NewCypress(RunnerConfig{
		TestCommand: "yarn cypress run --spec {{testExamples}}",
	})

	files := []string{"./cypress/e2e/passing_spec.cy.js"}
	got, err := cypress.Run(files, false)

	want := RunResult{
		Status: RunStatusPassed,
	}

	if err != nil {
		t.Errorf("Cypress.Run(%q) error = %v", files, err)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cypress.Run(%q) diff (-got +want):\n%s", files, diff)
	}
}

func TestCypressRun_TestFailed(t *testing.T) {
	changeCwd(t, "./testdata/cypress")

	cypress := NewCypress(RunnerConfig{
		TestCommand: "yarn cypress run --spec {{testExamples}}",
	})

	files := []string{"./cypress/e2e/failing_spec.cy.js"}
	got, err := cypress.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cypress.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Cypress.Run(%q) error type = %T (%v), want *exec.ExitError", files, err, err)
	}
}

func TestCypressRun_CommandFailed(t *testing.T) {
	cypress := Cypress{
		RunnerConfig{
			TestCommand: "yarn cypress run --json",
		},
	}

	files := []string{}
	got, err := cypress.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cypress.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	exitError := new(exec.ExitError)
	if !errors.As(err, &exitError) {
		t.Errorf("Cypress.Run(%q) error type = %T (%v), want *exec.ExitError", files, err, err)
	}
}

func TestCypressRun_SignaledError(t *testing.T) {
	cypress := NewCypress(RunnerConfig{
		TestCommand: "./testdata/segv.sh",
	})
	files := []string{"./doesnt-matter.cy.js"}

	got, err := cypress.Run(files, false)

	want := RunResult{
		Status: RunStatusError,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cypress.Run(%q) diff (-got +want):\n%s", files, diff)
	}

	signalError := new(ProcessSignaledError)
	if !errors.As(err, &signalError) {
		t.Errorf("Cypress.Run(%q) error type = %T (%v), want *ErrProcessSignaled", files, err, err)
	}
	if signalError.Signal != syscall.SIGSEGV {
		t.Errorf("Cypress.Run(%q) signal = %d, want %d", files, syscall.SIGSEGV, signalError.Signal)
	}
}

func TestCypressGetFiles(t *testing.T) {
	cypress := NewCypress(RunnerConfig{})

	got, err := cypress.GetFiles()
	if err != nil {
		t.Errorf("Cypress.GetFiles() error = %v", err)
	}

	want := []string{
		"testdata/cypress/cypress/e2e/failing_spec.cy.js",
		"testdata/cypress/cypress/e2e/flaky_spec.cy.js",
		"testdata/cypress/cypress/e2e/passing_spec.cy.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Cypress.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestCypressCommandNameAndArgs_WithInterpolationPlaceholder(t *testing.T) {
	testCases := []string{"cypress/e2e/passing_spec.cy.js", "cypress/e2e/flaky_spec.cy.js"}
	testCommand := "cypress run --spec {{testExamples}}"

	cy := Cypress{
		RunnerConfig{
			TestCommand: testCommand,
			ResultPath:  "cypress.json",
		},
	}

	gotName, gotArgs, err := cy.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "cypress"
	wantArgs := []string{"run", "--spec", "cypress/e2e/passing_spec.cy.js", "cypress/e2e/flaky_spec.cy.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCypressCommandNameAndArgs_WithoutTestExamplesPlaceholder(t *testing.T) {
	testCases := []string{"cypress/e2e/passing_spec.cy.js", "cypress/e2e/flaky_spec.cy.js"}
	testCommand := "cypress run"

	cypress := Cypress{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

	gotName, gotArgs, err := cypress.commandNameAndArgs(testCommand, testCases)
	if err != nil {
		t.Errorf("commandNameAndArgs(%q, %q) error = %v", testCases, testCommand, err)
	}

	wantName := "cypress"
	wantArgs := []string{"run", "--spec", "cypress/e2e/passing_spec.cy.js", "cypress/e2e/flaky_spec.cy.js"}

	if diff := cmp.Diff(gotName, wantName); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs(%q, %q) diff (-got +want):\n%s", testCases, testCommand, diff)
	}
}

func TestCypressCommandNameAndArgs_InvalidTestCommand(t *testing.T) {
	testCases := []string{"cypress/e2e/passing_spec.cy.js", "cypress/e2e/flaky_spec.cy.js"}
	testCommand := "cypress run --options '{{testExamples}}"

	cypress := Cypress{
		RunnerConfig{
			TestCommand: testCommand,
		},
	}

	gotName, gotArgs, err := cypress.commandNameAndArgs(testCommand, testCases)

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
