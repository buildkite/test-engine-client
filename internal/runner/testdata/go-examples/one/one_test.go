package one

import (
	"fmt"
	"os"
	"testing"
)

func TestAlpha(t *testing.T) {}

func TestAlphaExtra(t *testing.T) { t.Fatal("unassigned prefix match") }

func TestShared(t *testing.T) { t.Fatal("unassigned cross-package match") }

func TestDynamic(t *testing.T) {
	for _, name := range []string{"dot.+", "duplicate", "duplicate", "nested/child"} {
		t.Run(name, func(t *testing.T) {})
	}
}

func TestFlaky(t *testing.T) {
	t.Run("child", func(t *testing.T) {
		path := os.Getenv("GO_SPLIT_FLAKY_MARKER")
		if path == "" {
			t.Fatal("missing flaky test marker path")
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Fatal("first execution fails")
		}
	})
}

func Example() {
	fmt.Println("example")
	// Output: example
}

func FuzzSeed(f *testing.F) {
	f.Add("seed")
	f.Fuzz(func(t *testing.T, value string) {})
}

func BenchmarkNotATest(b *testing.B) {}
