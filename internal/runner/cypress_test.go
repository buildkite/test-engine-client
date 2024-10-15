package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCypressRun(t *testing.T) {
	mockCwd(t, "./testdata/cypress")

	cypress := NewCypress(RunnerConfig{})

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
