package runner

import "github.com/buildkite/test-engine-client/v2/internal/plan"

type TestRunner interface {
	Run(result *RunResult, testCases []plan.TestCase, retry bool) error
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	Name() string
	CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error)
	LocationPrefix() string
	UploadToken() string
}
