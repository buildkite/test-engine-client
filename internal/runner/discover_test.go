package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiscoverTestFiles(t *testing.T) {
	pattern := "test/**/*_test"
	got, err := discoverTestFiles(pattern, "")

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, "", err)
	}

	want := []string{
		"test/animals/ant_test",
		"test/animals/bee_test",
		"test/fruits/apple_test",
		"test/fruits/banana_test",
		"test/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, "", diff)
	}
}

func TestDiscoverTestFiles_WithExcludePattern(t *testing.T) {
	pattern := "test/**/*_test"
	excludePattern := "test/animals/*"
	got, err := discoverTestFiles(pattern, excludePattern)

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, excludePattern, err)
	}

	want := []string{
		"test/fruits/apple_test",
		"test/fruits/banana_test",
		"test/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, excludePattern, diff)
	}
}
