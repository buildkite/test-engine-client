package config

// EnvPayload is the data the client sends to the Test Engine API in the "env"
// field of requests (currently the filter_tests and test_plan_metadata
// endpoints).
//
// It is deliberately a separate type from Config so that the API contract is
// explicit and opt-in: Config is the client's internal configuration and is
// never serialized to the wire, whereas every field here is intentionally part
// of the request payload. Adding a new field to Config does NOT cause it to be
// sent to the API; it must be added here and mapped in Config.EnvPayload.
//
// The JSON keys below preserve the exact wire format that was previously
// produced by serializing Config directly, so existing API calls are
// unaffected.
type EnvPayload struct {
	BuildID                string `json:"BUILDKITE_BUILD_ID"`
	DebugEnabled           bool   `json:"BUILDKITE_TEST_ENGINE_DEBUG_ENABLED"`
	Identifier             string `json:"BUILDKITE_TEST_ENGINE_IDENTIFIER"`
	JobID                  string `json:"BUILDKITE_JOB_ID"`
	JobRetryCount          int    `json:"BUILDKITE_RETRY_COUNT"`
	MaxRetries             int    `json:"BUILDKITE_TEST_ENGINE_RETRY_COUNT"`
	NodeIndex              int    `json:"BUILDKITE_PARALLEL_JOB"`
	OrganizationSlug       string `json:"BUILDKITE_ORGANIZATION_SLUG"`
	Parallelism            int    `json:"BUILDKITE_PARALLEL_JOB_COUNT"`
	RetryCommand           string `json:"BUILDKITE_TEST_ENGINE_RETRY_CMD"`
	SplitByExample         bool   `json:"BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE"`
	StepID                 string `json:"BUILDKITE_STEP_ID"`
	SuiteSlug              string `json:"BUILDKITE_TEST_ENGINE_SUITE_SLUG"`
	TagFilters             string `json:"BUILDKITE_TEST_ENGINE_TAG_FILTERS"`
	TestCommand            string `json:"BUILDKITE_TEST_ENGINE_TEST_CMD"`
	TestFileExcludePattern string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN"`
	TestFilePattern        string `json:"BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN"`
	TestRunner             string `json:"BUILDKITE_TEST_ENGINE_TEST_RUNNER"`
}

// EnvPayload builds the API "env" payload from the client configuration. This
// is the single, deliberate boundary where internal configuration crosses into
// an API request body.
func (c *Config) EnvPayload() EnvPayload {
	return EnvPayload{
		BuildID:                c.BuildID,
		DebugEnabled:           c.DebugEnabled,
		Identifier:             c.Identifier,
		JobID:                  c.JobID,
		JobRetryCount:          c.JobRetryCount,
		MaxRetries:             c.MaxRetries,
		NodeIndex:              c.NodeIndex,
		OrganizationSlug:       c.OrganizationSlug,
		Parallelism:            c.Parallelism,
		RetryCommand:           c.RetryCommand,
		SplitByExample:         c.SplitByExample,
		StepID:                 c.StepID,
		SuiteSlug:              c.SuiteSlug,
		TagFilters:             c.TagFilters,
		TestCommand:            c.TestCommand,
		TestFileExcludePattern: c.TestFileExcludePattern,
		TestFilePattern:        c.TestFilePattern,
		TestRunner:             c.TestRunner,
	}
}
