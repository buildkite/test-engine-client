package runner

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiscoverTestFiles_WithDefault(t *testing.T) {
	defaultGlob := "fixtures/**/*_test"

	got, err := discoverTestFiles(defaultGlob)

	if err != nil {
		t.Errorf("discoverTestFiles(%q) error: %v", defaultGlob, err)
	}

	want := []string{
		"fixtures/animals/ant_test",
		"fixtures/animals/bee_test",
		"fixtures/fruits/apple_test",
		"fixtures/fruits/banana_test",
		"fixtures/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q) diff (-got +want):\n%s", defaultGlob, diff)
	}
}

func TestDiscoverTestFiles(t *testing.T) {
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", "fixtures/**/*_test")

	got, err := discoverTestFiles("foobar")

	if err != nil {
		t.Errorf("discoverTestFiles(%q) error: %v", "foobar", err)
	}

	want := []string{
		"fixtures/animals/ant_test",
		"fixtures/animals/bee_test",
		"fixtures/fruits/apple_test",
		"fixtures/fruits/banana_test",
		"fixtures/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q) diff (-got +want):\n%s", "foobar", diff)
	}
}

func TestDiscoverTestFiles_WithExcludePattern(t *testing.T) {
	pattern := "fixtures/**/*_test"
	excludePattern := "fixtures/animals/*"

	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", pattern)
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN", excludePattern)

	got, err := discoverTestFiles("foobar")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"fixtures/fruits/apple_test",
		"fixtures/fruits/banana_test",
		"fixtures/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}

func TestDiscoverTestFiles_WithExcludeDirectory(t *testing.T) {
	pattern := "fixtures/**/*_test"
	excludePattern := "fixtures/animals"

	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_PATTERN", pattern)
	os.Setenv("BUILDKITE_SPLITTER_TEST_FILE_EXCLUDE_PATTERN", excludePattern)

	got, err := discoverTestFiles("foobar")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"fixtures/fruits/apple_test",
		"fixtures/fruits/banana_test",
		"fixtures/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}
