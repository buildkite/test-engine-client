package runner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiscoverTestFiles(t *testing.T) {
	pattern := "fixtures/**/*_test"
	got, err := discoverTestFiles(DiscoveryPattern{
		IncludePattern: pattern,
		ExcludePattern: "",
	})

	if err != nil {
		t.Errorf("discoverTestFiles(%q, %q) error: %v", pattern, "", err)
	}

	want := []string{
		"fixtures/animals/ant_test",
		"fixtures/animals/bee_test",
		"fixtures/fruits/apple_test",
		"fixtures/fruits/banana_test",
		"fixtures/vegetable_test",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("discoverTestFiles(%q, %q) diff (-got +want):\n%s", pattern, "", diff)
	}
}

func TestDiscoverTestFiles_WithExcludePattern(t *testing.T) {
	pattern := "fixtures/**/*_test"
	excludePattern := "fixtures/animals/*"
	got, err := discoverTestFiles(DiscoveryPattern{
		IncludePattern: pattern,
		ExcludePattern: excludePattern,
	})

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
