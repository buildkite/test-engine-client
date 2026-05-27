package runner

import "github.com/buildkite/test-engine-client/v2/internal/plan"

type TestRunner interface {
	Run(result *RunResult, testCases []plan.TestCase, retry bool) error
	GetExamples(files []string) ([]plan.TestCase, error)
	GetFiles() ([]string, error)
	Name() string
	CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error)
	LocationPrefix() string
	SupportedFeatures() SupportedFeatures
	UploadToken() string
	// ResultFormat returns the format identifier for the runner's raw result file
	// (e.g. "rspec-json"), used when uploading results to Test Engine.
	// Returns an empty string if the runner does not support raw result uploads.
	ResultFormat() string
	// ResultFilePath returns the path to the runner's raw result file.
	ResultFilePath() string
}
