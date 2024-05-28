package plan

import (
	"cmp"
	"slices"
	"strconv"
)

// CreateFallbackPlan creates a fallback test plan for the given tests and parallelism.
// It distributes test cases evenly accross the tasks using deterministic algorithm.
func CreateFallbackPlan(files []string, parallelism int) TestPlan {
	// sort all test cases
	slices.SortFunc(files, func(a, b string) int {
		return cmp.Compare(a, b)
	})

	tasks := make(map[string]*Task)
	for i := 0; i < parallelism; i++ {
		tasks[strconv.Itoa(i)] = &Task{
			NodeNumber: i,
			Tests:      []TestCase{},
		}
	}

	// distribute files to tasks
	for i, file := range files {
		nodeNumber := i % parallelism
		task := tasks[strconv.Itoa(nodeNumber)]
		task.Tests = append(task.Tests, TestCase{
			Path: file,
		})
	}

	return TestPlan{
		Tasks: tasks,
	}
}
