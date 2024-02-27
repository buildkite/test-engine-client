package api

import (
	"sort"
	"strconv"
)

// CreateFallbackPlan creates a fallback test plan for the given tests and parallelism.
// It distributes test cases evenly accross the tasks using deterministic algorithm.
func CreateFallbackPlan(tests Tests, parallelism int) TestPlan {
	// sort all test cases
	testCases := tests.Cases
	sort.Slice(testCases, func(i, j int) bool {
		return testCases[i].Path < testCases[j].Path
	})

	// create tasks
	var tasks = make(map[string]Task)
	for i := 0; i < parallelism; i++ {
		tasks[strconv.Itoa(i)] = Task{
			NodeNumber: i,
			Tests: Tests{
				Format: "files",
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

	plan := TestPlan{
		Tasks: tasks,
	}

	return plan
}
