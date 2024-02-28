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
	}

	for _, s := range scenarios {
		tests := Tests{
			Cases:  s.testCases,
			Format: "files",
		}

		plan := CreateFallbackPlan(tests, s.parallelism)
		got := make([][]TestCase, s.parallelism)
		for _, task := range plan.Tasks {
			got[task.NodeNumber] = task.Tests.Cases
		}

		if diff := cmp.Diff(got, s.want); diff != "" {
			t.Errorf("CreateFallbackPlan(%v, %v) diff (-got +want):\n%s", s.testCases, s.parallelism, diff)
		}
	}
}
