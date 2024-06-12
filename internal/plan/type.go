package plan

type TestCaseFormat string

const (
	TestCaseFormatFile    TestCaseFormat = "file"
	TestCaseFormatExample TestCaseFormat = "example"
)

// TestCase represents a single test case.
type TestCase struct {
	Path              string         `json:"path"`
	EstimatedDuration int            `json:"estimated_duration,omitempty"`
	Format            TestCaseFormat `json:"format,omitempty"`
}

// Task represents the task for the given node.
type Task struct {
	NodeNumber int        `json:"node_number"`
	Tests      []TestCase `json:"tests"`
}

// TestPlan represents the entire test plan.
type TestPlan struct {
	Tasks map[string]*Task `json:"tasks"`
}
