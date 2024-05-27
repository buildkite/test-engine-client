package plan

type TestCaseFormat string

const (
	TestCaseFormatFile    TestCaseFormat = "file"
	TestCaseFormatExample TestCaseFormat = "example"
)

// TestCase represents a single test case.
type TestCase struct {
	EstimatedDuration *int           `json:"estimated_duration,omitempty"`
	Format            TestCaseFormat `json:"format,omitempty"`
	Identifier        string         `json:"identifier,omitempty"`
	Name              string         `json:"name,omitempty"`
	Path              string         `json:"path"`
	Scope             string         `json:"scope,omitempty"`
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
