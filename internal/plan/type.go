package plan

type TestCaseFormat string

const (
	TestCaseFormatFile    TestCaseFormat = "file"
	TestCaseFormatExample TestCaseFormat = "example"
)

// TestCase represents a single test case.
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
	Path  string `json:"path"`
	Scope string `json:"scope,omitempty"`
}

// Task represents the task for the given node.
type Task struct {
	NodeNumber int        `json:"node_number"`
	Tests      []TestCase `json:"tests"`
}

// TestPlan represents the entire test plan.
type TestPlan struct {
	Experiment   string           `json:"experiment"`
	Tasks        map[string]*Task `json:"tasks"`
	Fallback     bool
	MutedTests   []TestCase `json:"muted_tests,omitempty"`
	SkippedTests []TestCase `json:"skipped_tests,omitempty"`
}
