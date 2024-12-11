package testdata

import "testing"

func TestFailing(t *testing.T) {
	t.Error("This test should fail")
}
