package plan

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateFallbackPlan(t *testing.T) {
	scenarios := []struct {
		files       []string
		parallelism int
		want        [][]TestCase
	}{
		{
			files:       []string{"a", "b", "c", "d", "e"},
			parallelism: 2,
			want: [][]TestCase{
				{{Path: "a"}, {Path: "c"}, {Path: "e"}},
				{{Path: "b"}, {Path: "d"}},
			},
		},
		{
			files:       []string{"a", "c", "b", "e", "d"},
			parallelism: 3,
			want: [][]TestCase{
				{{Path: "a"}, {Path: "d"}},
				{{Path: "b"}, {Path: "e"}},
				{{Path: "c"}},
			},
		},
		// The function should allow for duplicate test cases in the input
		// and distribute them evenly across the nodes.
		// This can be useful when the same test case needs to be run multiple times
		// to ensure it's not flaky.
		// Preventing duplicate test cases will be the responsibility of the caller.
		{
			files:       []string{"a", "a", "b", "c", "d", "c"},
			parallelism: 4,
			want: [][]TestCase{
				{{Path: "a"}, {Path: "c"}},
				{{Path: "a"}, {Path: "d"}},
				{{Path: "b"}},
				{{Path: "c"}},
			},
		},
		{
			files:       []string{"a", "b"},
			parallelism: 3,
			want: [][]TestCase{
				{{Path: "a"}},
				{{Path: "b"}},
				{},
			},
		},
	}

	for _, s := range scenarios {
		plan := CreateFallbackPlan(s.files, s.parallelism)
		got := make([][]TestCase, s.parallelism)
		for _, task := range plan.Tasks {
			got[task.NodeNumber] = task.Tests
		}

		if diff := cmp.Diff(got, s.want); diff != "" {
			t.Errorf("CreateFallbackPlan(%v, %v) diff (-got +want):\n%s", s.files, s.parallelism, diff)
		}
	}
}
