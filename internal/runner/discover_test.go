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
