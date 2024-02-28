package plan

import (
	"cmp"
	"slices"
	"strconv"
)

// CreateFallbackPlan creates a fallback test plan for the given tests and parallelism.
// It distributes test cases evenly accross the tasks using deterministic algorithm.
func CreateFallbackPlan(tests Tests, parallelism int) TestPlan {
	// sort all test cases
	testCases := tests.Cases
	slices.SortFunc(testCases, func(a, b TestCase) int {
		return cmp.Compare(a.Path, b.Path)
	})

	tasks := make(map[string]Task)
	for i := 0; i < parallelism; i++ {
		tasks[strconv.Itoa(i)] = Task{
			NodeNumber: i,
			Tests: Tests{
				Format: tests.Format,
				Cases:  []TestCase{},
			},
		}
	}

	// distribute test cases to tasks
	for i, testCase := range testCases {
		nodeNumber := i % parallelism
		task := tasks[strconv.Itoa(nodeNumber)]
		task.Tests.Cases = append(task.Tests.Cases, testCase)
		tasks[strconv.Itoa(nodeNumber)] = task
	}

	return TestPlan{
		Tasks: tasks,
	}
}
