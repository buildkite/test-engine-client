package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiscoverTestFiles(t *testing.T) {
	pattern := "testdata/files/**/*_test"
	got, err := discoverTestFiles(pattern, "")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, "", err)
	}

	want := []string{
		"testdata/files/animals/ant_test",
		"testdata/files/animals/bee_test",
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
		"testdata/files/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, "", diff)
	}
}

func TestDiscoverTestFiles_WithExcludePattern(t *testing.T) {
	pattern := "testdata/files/**/*_test"
	excludePattern := "testdata/files/**/animals/*"
	got, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
		"testdata/files/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}

func TestDiscoverTestFiles_WithExcludeDirectory(t *testing.T) {
	pattern := "testdata/files/**/*_test"
	excludePattern := "testdata/files/**/animals"
	got, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
		"testdata/files/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}

func TestDiscoverTestFiles_ExcludeNodeModules(t *testing.T) {
	pattern := "testdata/**/*.js"
	excludePattern := ""
	got, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"testdata/cypress/cypress/e2e/failing_spec.cy.js",
		"testdata/cypress/cypress/e2e/flaky_spec.cy.js",
		"testdata/cypress/cypress/e2e/passing_spec.cy.js",
		"testdata/cypress/cypress.config.js",
		"testdata/jest/failure.spec.js",
		"testdata/jest/jest.config.js",
		"testdata/jest/runtimeError.spec.js",
		"testdata/jest/skipped.spec.js",
		"testdata/jest/slow.spec.js",
		"testdata/jest/spells/expelliarmus.spec.js",
		"testdata/playwright/playwright.config.js",
		"testdata/playwright/tests/error.spec.js",
		"testdata/playwright/tests/example.spec.js",
		"testdata/playwright/tests/failed.spec.js",
		"testdata/playwright/tests/skipped.spec.js",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}

func TestDiscoverTestFiles_CommaSeparatedPatterns(t *testing.T) {
	pattern := "testdata/files/animals/*_test, testdata/files/fruits/*_test"
	got, err := discoverTestFiles(pattern, "")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, "", err)
	}

	want := []string{
		"testdata/files/animals/ant_test",
		"testdata/files/animals/bee_test",
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, "", diff)
	}
}

func TestDiscoverTestFiles_CommaSeparatedWithDuplicates(t *testing.T) {
	pattern := "testdata/files/**/*_test,testdata/files/animals/*_test"
	got, err := discoverTestFiles(pattern, "")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, "", err)
	}

	want := []string{
		"testdata/files/animals/ant_test",
		"testdata/files/animals/bee_test",
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
		"testdata/files/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, "", diff)
	}
}

func TestDiscoverTestFiles_CommaSeparatedWithExclude(t *testing.T) {
	pattern := "testdata/files/animals/*_test,testdata/files/fruits/*_test"
	excludePattern := "testdata/files/**/ant_test"
	got, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"testdata/files/animals/bee_test",
		"testdata/files/fruits/apple_test",
		"testdata/files/fruits/banana_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}
