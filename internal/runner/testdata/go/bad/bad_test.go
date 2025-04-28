package bad

import (
	"testing"
)

func TestBad(t *testing.T) {
	t.Errorf("Test failed: expected %v, got %v", true, false)
}
