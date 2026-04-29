package plan

type TestCaseFormat string

const (
	TestCaseFormatFile    TestCaseFormat = "file"
	TestCaseFormatExample TestCaseFormat = "example"
)

// TestCase currently can represent a single test case or a single test file (when used as output of test plan API).
// TODO: it's best if we split this into two types.
type TestCase struct {
	EstimatedDuration int            `json:"estimated_duration,omitempty"`
	Format            TestCaseFormat `json:"format,omitempty"`
	Identifier        string         `json:"identifier,omitempty"`
	Name              string         `json:"name,omitempty"`
	// Path is the path of the individual test or test file that the test runner can interpret.
	// For example:
	// In RSpec, the path can be a test file like `user_spec.rb` or an individual test id like `user_spec.rb[1,2]`.
	// In Jest, the path is a test file like `src/components/Button.spec.tsx`.
	// In pytest, the path can be a test file like `test_hello.py` or a node id like `test_hello.py::TestHello::test_greet`
	// In go test, the path can only be package name like "example.com/foo/bar".
	Path  string `json:"path"`
	Scope string `json:"scope,omitempty"`
	// TimingSampleSize is the number of historical executions/runs behind this
	// case's EstimatedDuration. For file-scoped cases this is distinct runs,
	// for example-scoped cases this is raw executions. Defaults to 0 when no
	// history is available or the field is missing on older/cached plans.
	TimingSampleSize int `json:"timing_sample_size"`
}

// TimingMetadata describes the historical timing data the server used to
// build the test plan. All durations are in milliseconds and may be
// fractional (the server-side median can be the mean of two middle values).
type TimingMetadata struct {
	// MedianDuration is the median of historical timings used to backfill
	// cases without history. Nil when no history existed at all.
	MedianDuration *float64 `json:"median_duration"`
	// DefaultDuration is the assumed duration when no history exists at all.
	DefaultDuration float64 `json:"default_duration"`
}

// Task represents the task for the given node.
type Task struct {
	NodeNumber int `json:"node_number"`
	// When splitting by file, this tests array is essentially an array of test files.
	// When splitting by example, this array is an array of proper test cases.
	// See comment above, we plan to split TestCase into two types or clarify its usage.
	Tests []TestCase `json:"tests"`
}

// TestPlan represents the entire test plan.
type TestPlan struct {
	Identifier   string           `json:"identifier"`
	Parallelism  int              `json:"parallelism"`
	Experiment   string           `json:"experiment"`
	Tasks        map[string]*Task `json:"tasks"`
	Fallback     bool
	MutedTests   []TestCase `json:"muted_tests,omitempty"`
	SkippedTests []TestCase `json:"skipped_tests,omitempty"`
	// TimingMetadata describes the historical timing data the server used to
	// build this plan. Nil when missing (e.g. error plans, plans cached before
	// the server began emitting it).
	TimingMetadata *TimingMetadata `json:"timing_metadata,omitempty"`
}
