package pkg1

import "testing"

func TestPkg1A(t *testing.T) {
	// This test should pass
}

func TestPkg1B(t *testing.T) {
	t.Error("This test should fail")
}
