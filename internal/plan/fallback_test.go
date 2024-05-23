package plan

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateFallbackPlan(t *testing.T) {
	scenarios := []struct {
		testCases   []TestCase
		parallelism int
		want        [][]TestCase
	}{
		{
			testCases: []TestCase{
				{Path: "a"},
				{Path: "b"},
				{Path: "c"},
				{Path: "d"},
				{Path: "e"},
			},
			parallelism: 2,
			want: [][]TestCase{
				{{Path: "a"}, {Path: "c"}, {Path: "e"}},
				{{Path: "b"}, {Path: "d"}},
			},
		},
		{
			testCases: []TestCase{
				{Path: "a"},
				{Path: "c"},
				{Path: "b"},
				{Path: "e"},
				{Path: "d"},
			},
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
			testCases: []TestCase{
				{Path: "a"},
				{Path: "a"},
				{Path: "b"},
				{Path: "c"},
				{Path: "d"},
				{Path: "c"},
			},
			parallelism: 4,
			want: [][]TestCase{
				{{Path: "a"}, {Path: "c"}},
				{{Path: "a"}, {Path: "d"}},
				{{Path: "b"}},
				{{Path: "c"}},
			},
		},
		{
			testCases: []TestCase{
				{Path: "a"},
				{Path: "b"},
			},
			parallelism: 3,
			want: [][]TestCase{
				{{Path: "a"}},
				{{Path: "b"}},
				{},
			},
		},
	}

	for _, s := range scenarios {
		plan := CreateFallbackPlan(s.testCases, s.parallelism)
		got := make([][]TestCase, s.parallelism)
		for _, task := range plan.Tasks {
			got[task.NodeNumber] = task.Tests
		}

		if diff := cmp.Diff(got, s.want); diff != "" {
			t.Errorf("CreateFallbackPlan(%v, %v) diff (-got +want):\n%s", s.testCases, s.parallelism, diff)
		}
	}
}
