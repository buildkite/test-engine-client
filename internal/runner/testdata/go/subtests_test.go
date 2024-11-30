package testdata

import "testing"

func TestWithSubtests(t *testing.T) {
	t.Run("SubtestA", func(t *testing.T) {
		// This subtest should pass
	})

	t.Run("SubtestB", func(t *testing.T) {
		t.Error("This subtest should fail")
	})
}
