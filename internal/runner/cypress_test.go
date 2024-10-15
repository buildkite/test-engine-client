package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCypressRun(t *testing.T) {
	mockCwd(t, "./testdata/cypress")

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
