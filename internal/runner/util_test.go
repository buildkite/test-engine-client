package runner

import (
	"os"
	"testing"
)

// mockCwd changes the current working directory to the given path for the duration of the test.
// This is useful for tests that need to run in a specific directory, for example to test the runner.
func mockCwd(t *testing.T, path string) {
	t.Helper()
	origWD, err := os.Getwd()

	if err != nil {
		t.Fatal(err)
	}

	err = os.Chdir(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})
}
