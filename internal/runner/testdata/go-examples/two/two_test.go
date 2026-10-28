package two

import "testing"

func TestShared(t *testing.T) {}

func TestAlpha(t *testing.T) { t.Fatal("unassigned cross-package match") }
