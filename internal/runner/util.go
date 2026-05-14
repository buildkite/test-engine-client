package runner

import "github.com/buildkite/test-engine-client/v2/internal/plan"

func testCasesFromPaths(paths []string) []plan.TestCase {
	testCases := make([]plan.TestCase, len(paths))
	for i, path := range paths {
		testCases[i] = plan.TestCase{Path: path}
	}
	return testCases
}

func pathsFromTestCases(testCases []plan.TestCase) []string {
	paths := make([]string, len(testCases))
	for i, tc := range testCases {
		paths[i] = tc.Path
	}
	return paths
}
