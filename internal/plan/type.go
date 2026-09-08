package plan

type TestCaseFormat string

const (
	TestCaseFormatFile     TestCaseFormat = "file"
	TestCaseFormatExample  TestCaseFormat = "example"
	TestCaseFormatSelector TestCaseFormat = "selector"
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
	// Value is the runnable selector for selector-based plans, for example a Go package import path.
	Value string `json:"value,omitempty"`
	// TimingSampleSize is the number of historical executions/runs behind this
	// case's EstimatedDuration. For file-scoped cases this is distinct runs,
	// for example-scoped cases this is raw executions. Defaults to 0 when no
	// history is available or the field is missing on older/cached plans.
	TimingSampleSize int `json:"timing_sample_size,omitempty"`
}

// TimingMetadata describes the historical timing data the server used to
// build the test plan, broken down per case format. Either key may be
// omitted when the plan contains no cases of that format (or when
// parallelism is 1 and no timings were fetched).
type TimingMetadata struct {
	File     *FormatTimingMetadata `json:"file,omitempty"`
	Example  *FormatTimingMetadata `json:"example,omitempty"`
	Selector *FormatTimingMetadata `json:"selector,omitempty"`
}

// FormatTimingMetadata is the timing data for a single case format
// (file-scoped or example-scoped). All durations are in milliseconds and
// may be fractional (the server-side median can be the mean of two middle
// values).
type FormatTimingMetadata struct {
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

// SelectionMetadata contains public selection outcomes. Pointers distinguish
// missing/null metadata on older plans from real zero counts and false outcomes.
// Counts are runnable units, not necessarily paths or the cutoff's denominator.
type SelectionMetadata struct {
	Applied                  *bool                       `json:"applied,omitempty"`
	Strategy                 *string                     `json:"strategy,omitempty"`
	CandidateCount           *int                        `json:"candidate_count,omitempty"`
	SelectedCount            *int                        `json:"selected_count,omitempty"`
	ScoreCutoff              *float64                    `json:"score_cutoff,omitempty"`
	CountCutoff              *int                        `json:"count_cutoff,omitempty"`
	ProportionCutoff         *float64                    `json:"proportion_cutoff,omitempty"`
	DurationProportionCutoff *float64                    `json:"duration_proportion_cutoff,omitempty"`
	EffectiveCount           *int                        `json:"effective_count,omitempty"`
	SkippedReason            *string                     `json:"skipped_reason,omitempty"`
	DurationEstimates        *SelectionDurationEstimates `json:"duration_estimates,omitempty"`
}

// SelectionDurationEstimates prices both pools using candidate-pool fallbacks.
// The top-level duration_estimates uses different, selected-pool fallbacks and
// must not supply either side of this share. Zero candidate compute has no share.
type SelectionDurationEstimates struct {
	Estimator                string   `json:"estimator,omitempty"`
	CandidateTotalDurationMS *int     `json:"candidate_total_duration_ms,omitempty"`
	SelectedTotalDurationMS  *int     `json:"selected_total_duration_ms,omitempty"`
	CandidateTimingCoverage  *float64 `json:"candidate_timing_coverage,omitempty"`
}

// SizingMetadata records the actual server sizing branch, never reconstructed
// from settings or tasks. Binding flags are independent: tied constraints are
// both false. Target/required nodes exist only when timing-based sizing was used.
type SizingMetadata struct {
	Method                       string   `json:"method,omitempty"`
	RunnableUnits                *int     `json:"runnable_units,omitempty"`
	MaxParallelismBinding        *bool    `json:"max_parallelism_binding,omitempty"`
	RunnableUnitsBinding         *bool    `json:"runnable_units_binding,omitempty"`
	TargetTimeSource             string   `json:"target_time_source,omitempty"`
	TargetTimeMS                 *float64 `json:"target_time_ms,omitempty"`
	EstimatedRequiredParallelism *int     `json:"estimated_required_parallelism,omitempty"`
	Estimator                    string   `json:"estimator,omitempty"`
	EstimatedMaxTaskDurationMS   *int     `json:"estimated_max_task_duration_ms,omitempty"`
}

// Settings describes returned split constraints, not the current invocation.
type Settings struct {
	MaxParallelism *int     `json:"max_parallelism,omitempty"`
	TargetTime     *float64 `json:"target_time,omitempty"` // seconds
}

// TestPlan represents the entire test plan.
type TestPlan struct {
	Identifier   string           `json:"identifier"`
	Parallelism  int              `json:"parallelism"`
	Experiment   string           `json:"experiment"`
	Tasks        map[string]*Task `json:"tasks"`
	Fallback     bool
	MutedTests   []TestCase         `json:"muted_tests,omitempty"`
	SkippedTests []TestCase         `json:"skipped_tests,omitempty"`
	Selection    *SelectionMetadata `json:"selection,omitempty"`
	Settings     *Settings          `json:"settings,omitempty"`
	Sizing       *SizingMetadata    `json:"sizing,omitempty"`
	// TimingMetadata describes the historical timing data the server used to
	// build this plan. Nil when missing (e.g. error plans, plans cached before
	// the server began emitting it).
	TimingMetadata *TimingMetadata `json:"timing_metadata,omitempty"`
	// KnownTimingsRatio is the fraction (0.0–1.0) of cases that had historical
	// timing data when the plan was built. The server only emits it at
	// parallelism > 1, where the split summary instead uses per-format
	// TimingMetadata. Currently unread; retained for wire compatibility.
	KnownTimingsRatio *float64 `json:"known_timings_ratio,omitempty"`
}
