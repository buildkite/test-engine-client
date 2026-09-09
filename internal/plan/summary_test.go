package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestPrintSplitSummary_MixedHistory(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a", TimingSampleSize: 5},
				{Path: "b", TimingSampleSize: 3},
				{Path: "c", TimingSampleSize: 0},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Path: "d", TimingSampleSize: 1},
				{Path: "e", TimingSampleSize: 0},
			}},
		},
		TimingMetadata: &TimingMetadata{
			File: &FormatTimingMetadata{MedianDuration: fp(4200), DefaultDuration: 1000},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"Split summary\n  5 files across 2 nodes",
		"Historical timings: 3 of 5 files (60%)",
		"No history: 2 files (40%); median duration 4.2s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestPrintSplitSummary_NoHistory(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a"}, {Path: "b"}}},
			"1": {NodeNumber: 1, Tests: []TestCase{{Path: "c"}}},
		},
		TimingMetadata: &TimingMetadata{
			File: &FormatTimingMetadata{MedianDuration: nil, DefaultDuration: 1000},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"Split summary\n  3 files across 2 nodes",
		"Historical timings: 0 of 3 files (0%)",
		"No history: 3 files (100%); default duration 1.0s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "estimated from past") {
		t.Errorf("unexpected estimated line in no-history output:\n%s", got)
	}
}

func TestPrintSplitSummary_NullMedianWithUnknowns(t *testing.T) {
	// In practice the server only sets median_duration=null when there is no
	// history at all, but the client should still degrade gracefully.
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a", TimingSampleSize: 1}}},
			"1": {NodeNumber: 1, Tests: []TestCase{{Path: "b", TimingSampleSize: 0}}},
		},
		TimingMetadata: &TimingMetadata{
			File: &FormatTimingMetadata{MedianDuration: nil, DefaultDuration: 1000},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	if strings.Contains(got, "median duration") {
		t.Errorf("expected no median when MedianDuration is nil, got:\n%s", got)
	}
	if !strings.Contains(got, "No history: 1 file (50%)\n") {
		t.Errorf("expected bare \"had no history\" line, got:\n%s", got)
	}
}

func TestPrintSplitSummary_NoMetadata(t *testing.T) {
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a"}}},
		},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	if !strings.Contains(buf.String(), "1 file across 1 node") || !strings.Contains(buf.String(), "Historical timings: unavailable") {
		t.Errorf("expected count and unknown history, got: %s", buf.String())
	}
}

func TestPrintSplitSummary_ParallelismOneNoBreakdown(t *testing.T) {
	// At parallelism=1 the server skips the per-format timing fetch and emits an
	// empty timing_metadata, so the summary prints only the header and count.
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a"}, {Path: "b"}, {Path: "c"}, {Path: "d"},
			}},
		},
		TimingMetadata: &TimingMetadata{},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	if !strings.Contains(got, "4 files across 1 node") {
		t.Errorf("output missing count line\nfull output:\n%s", got)
	}
	if strings.Contains(got, "Historical timings:") || strings.Contains(got, "No history:") {
		t.Errorf("unexpected breakdown at parallelism 1:\n%s", got)
	}
}

func TestPrintSplitSummary_ExampleMode(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a[1]", Format: TestCaseFormatExample, TimingSampleSize: 4},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Path: "a[2]", Format: TestCaseFormatExample, TimingSampleSize: 0},
			}},
		},
		TimingMetadata: &TimingMetadata{
			Example: &FormatTimingMetadata{MedianDuration: fp(2000), DefaultDuration: 1000},
		},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"Split summary\n  2 examples across 2 nodes",
		"Historical timings: 1 of 2 examples (50%)",
		"No history: 1 example (50%)",
		"2.0s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, " files ") {
		t.Errorf("expected no \"files\" in example-mode output, got:\n%s", got)
	}
}

func TestPrintSplitSummary_SelectorMode(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Value: "github.com/buildkite/test-engine-client/internal/api", Format: TestCaseFormatSelector, TimingSampleSize: 7},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Value: "github.com/buildkite/test-engine-client/internal/runner", Format: TestCaseFormatSelector, TimingSampleSize: 0},
			}},
		},
		TimingMetadata: &TimingMetadata{
			Selector: &FormatTimingMetadata{MedianDuration: fp(1750), DefaultDuration: 1000},
		},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"Split summary\n  2 selectors across 2 nodes",
		"Historical timings: 1 of 2 selectors (50%)",
		"No history: 1 selector (50%); median duration 1.8s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, " files ") || strings.Contains(got, " examples ") {
		t.Errorf("expected no file/example wording in selector-mode output, got:\n%s", got)
	}
	if p.HasNoSelectorTimingHistory() {
		t.Error("expected plan with selector history not to report missing selector timings")
	}
}

func TestPrintSplitSummary_SelectorModeNoHistoryUsesDefaultDuration(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Value: "spec/models/user_spec.rb", Format: TestCaseFormatSelector},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Value: "spec/models/team_spec.rb", Format: TestCaseFormatSelector},
			}},
		},
		TimingMetadata: &TimingMetadata{
			Selector: &FormatTimingMetadata{MedianDuration: nil, DefaultDuration: 1000},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"Historical timings: 0 of 2 selectors (0%)",
		"No history: 2 selectors (100%); default duration 1.0s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
	if !p.HasNoSelectorTimingHistory() {
		t.Error("expected plan to report no selector timing history")
	}
}

func TestPrintSplitSummary_SelectorModeParallelismOneDoesNotWarn(t *testing.T) {
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Value: "spec/models/user_spec.rb", Format: TestCaseFormatSelector},
			}},
		},
		TimingMetadata: &TimingMetadata{},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	if !strings.Contains(got, "1 selector across 1 node") {
		t.Errorf("output missing count line\nfull output:\n%s", got)
	}
	if strings.Contains(got, "update it and run the suite once") {
		t.Errorf("unexpected collector warning at parallelism 1, got:\n%s", got)
	}
}

func TestPrintSplitSummary_MixedFormats(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a_spec.rb", Format: TestCaseFormatFile, TimingSampleSize: 3},
				{Path: "b_spec.rb[1:1]", Format: TestCaseFormatExample, TimingSampleSize: 0},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Path: "c_spec.rb", Format: TestCaseFormatFile, TimingSampleSize: 0},
				{Path: "b_spec.rb[1:2]", Format: TestCaseFormatExample, TimingSampleSize: 2},
			}},
		},
		TimingMetadata: &TimingMetadata{
			File:    &FormatTimingMetadata{MedianDuration: fp(4200), DefaultDuration: 1000},
			Example: &FormatTimingMetadata{MedianDuration: fp(150), DefaultDuration: 500},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"4 test selectors across 2 nodes",
		"  2 files\n",
		"    Historical timings: 1 of 2 files (50%)",
		"    No history: 1 file (50%); median duration 4.2s",
		"  2 examples\n",
		"    Historical timings: 1 of 2 examples (50%)",
		"    No history: 1 example (50%); median duration 150ms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestPrintSplitSummary_MixedFormatsWithSelectors(t *testing.T) {
	p := TestPlan{
		Parallelism: 2,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a_spec.rb", Format: TestCaseFormatFile, TimingSampleSize: 3},
				{Value: "github.com/buildkite/test-engine-client/internal/api", Format: TestCaseFormatSelector, TimingSampleSize: 0},
			}},
			"1": {NodeNumber: 1, Tests: []TestCase{
				{Path: "b_spec.rb[1:1]", Format: TestCaseFormatExample, TimingSampleSize: 0},
				{Value: "github.com/buildkite/test-engine-client/internal/runner", Format: TestCaseFormatSelector, TimingSampleSize: 2},
			}},
		},
		TimingMetadata: &TimingMetadata{
			File:     &FormatTimingMetadata{MedianDuration: fp(4200), DefaultDuration: 1000},
			Example:  &FormatTimingMetadata{MedianDuration: nil, DefaultDuration: 500},
			Selector: &FormatTimingMetadata{MedianDuration: fp(1750), DefaultDuration: 1000},
		},
	}

	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"4 test selectors across 2 nodes",
		"  1 file\n",
		"    Historical timings: 1 of 1 file (100%)",
		"  1 example\n",
		"    No history: 1 example (100%); default duration 500ms",
		"  2 selectors\n",
		"    Historical timings: 1 of 2 selectors (50%)",
		"    No history: 1 selector (50%); median duration 1.8s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "update it and run the suite once") {
		t.Errorf("unexpected collector warning when selector history exists, got:\n%s", got)
	}
}

func TestPrintSplitSummary_Fallback(t *testing.T) {
	p := TestPlan{
		Parallelism: 1,
		Fallback:    true,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a"}}},
		},
		TimingMetadata: &TimingMetadata{
			File: &FormatTimingMetadata{DefaultDuration: 1000},
		},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	if !strings.Contains(buf.String(), "1 test selector across 1 node") || !strings.Contains(buf.String(), "Local non-intelligent split") {
		t.Errorf("expected local fallback summary, got: %s", buf.String())
	}
	if strings.Contains(buf.String(), "No history:") {
		t.Errorf("unexpected server timing breakdown for fallback: %s", buf.String())
	}
}

func TestPrintSelectionSummary(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"old plan", `{}`, "No selection metadata returned"},
		{"null", `{"selection":null}`, "No selection metadata returned"},
		{"missing applied", `{"selection":{"applied":null,"selected_count":0}}`, "Applied status: unavailable\n  Selected: 0 of unknown test selectors"},
		{"zero selected", `{"selection":{"applied":true,"candidate_count":5,"selected_count":0}}`, "Applied (strategy unavailable)\n  Selected: 0 of 5 test selectors (0%)"},
		{"zero eligible", `{"selection":{"applied":true,"candidate_count":0,"selected_count":0}}`, "Selected: 0 of 0 test selectors (percentage unavailable)"},
		{"full selection", `{"selection":{"applied":true,"candidate_count":5,"selected_count":5}}`, "Selected: 5 of 5 test selectors (100%)"},
		{"mixed denominator", `{"selection":{"applied":true,"candidate_count":6,"selected_count":2},"tasks":{"0":{"tests":[{"format":"file"},{"format":"example"}]}}}`, "Selected: 2 of 6 test selectors (33.3%)"},
		{"fallback reason", `{"selection":{"applied":false,"candidate_count":5,"selected_count":5,"skipped_reason":"no_model"}}`, "Not applied (strategy unavailable)\n  Reason: no_model\n  Selected: 5 of 5 test selectors (100%)"},
		{"zero cutoffs", `{"selection":{"score_cutoff":0,"count_cutoff":0,"proportion_cutoff":0,"duration_proportion_cutoff":0,"effective_count":0}}`, "Returned parameter: score_cutoff = 0\n  Returned parameter: count_cutoff = 0\n  Returned parameter: proportion_cutoff = 0\n  Returned parameter: duration_proportion_cutoff = 0\n  Effective count: 0"},
		{"null cutoffs", `{"selection":{"score_cutoff":null,"count_cutoff":null}}`, "Selected: unknown of unknown test selectors"},
		{"local fallback", `{"Fallback":true,"tasks":{"0":{"tests":[{"path":"a"}]}}}`, "Not applied: local fallback uses the full locally discovered suite."},
		{"taskless placeholder", `{"Fallback":true}`, "Not determined: taskless fallback placeholder."},
		{"empty task map placeholder", `{"Fallback":true,"tasks":{}}`, "Not determined: taskless fallback placeholder."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var p TestPlan
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p, nil)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("want %q, got:\n%s", tt.want, &buf)
			}
			if strings.Contains(buf.String(), "+++") || strings.Contains(buf.String(), "\n\n\n") {
				t.Errorf("redundant group or blank line: %q", buf.String())
			}
		})
	}
}

func TestPrintSplitSummary_Placeholder(t *testing.T) {
	for _, tasks := range []map[string]*Task{nil, {}} {
		p := TestPlan{Fallback: true, Parallelism: 3, Tasks: tasks}
		var buf bytes.Buffer
		PrintSelectionSummary(&buf, p, nil)
		PrintSplitSummary(&buf, p)
		assertSummary(t, buf.String(), []string{
			"Not determined: taskless fallback placeholder.",
			"Placeholder only: 3 nodes; no test allocation computed.",
		}, []string{"full locally discovered suite", "Local non-intelligent split", "0 nodes"})
	}
}

func TestSelectionReasonBoundedAndEscaped(t *testing.T) {
	reason := "no_model\n+++ forged\x1b" + strings.Repeat("x", 1000)
	p := TestPlan{Selection: &SelectionMetadata{SkippedReason: &reason}}
	var buf bytes.Buffer
	PrintSelectionSummary(&buf, p, nil)
	if !strings.Contains(buf.String(), `no_model\n+++ forged\x1b`) || len(buf.String()) > 400 {
		t.Fatalf("reason not safely bounded: %q", buf.String())
	}
}

func TestPrintSplitSummary_ReturnedConstraints(t *testing.T) {
	for _, tt := range []struct {
		body, want string
	}{
		{`{"parallelism":2,"settings":{"target_time":120.5,"max_parallelism":2}}`, "Target time: 120.5s (usage unknown)\n  Node limit: 2 (binding unknown)"},
		{`{"parallelism":2,"settings":{"max_parallelism":5}}`, "Node limit: 5 (binding unknown)"},
		{`{"parallelism":0,"tasks":{},"settings":{"target_time":0,"max_parallelism":0}}`, "Target time: 0s (usage unknown)\n  Node limit: 0 (binding unknown)"},
		{`{"parallelism":1,"settings":{"target_time":null,"max_parallelism":null}}`, "Node limit: unavailable (binding unknown)"},
	} {
		var p TestPlan
		if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		PrintSplitSummary(&buf, p)
		if !strings.Contains(buf.String(), tt.want) || strings.Contains(buf.String(), "capped at") {
			t.Errorf("unexpected constraints for %s:\n%s", tt.body, &buf)
		}
	}
}

func TestPercentOf(t *testing.T) {
	tests := []struct {
		name  string
		n     int
		total int
		want  string
	}{
		{name: "zero total", n: 0, total: 0, want: "0%"},
		{name: "whole percentage", n: 3, total: 5, want: "60%"},
		{name: "rounds to one decimal", n: 1, total: 3, want: "33.3%"},
		{name: "small non-zero percentage", n: 1, total: 200, want: "0.5%"},
		{name: "tiny non-zero percentage", n: 1, total: 10000, want: "<0.1%"},
		{name: "nearly all but not all", n: 9999, total: 10000, want: ">99.9%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentOf(tt.n, tt.total); got != tt.want {
				t.Errorf("percentOf(%d, %d) = %q, want %q", tt.n, tt.total, got, tt.want)
			}
		})
	}
}

// These are the exact response fragments from the verified TE-7042 backend
// handoff. In particular, the top-level mean estimate must not replace P90.
func planningMetadataFixture(t *testing.T, name string) TestPlan {
	t.Helper()
	body, err := os.ReadFile("testdata/planning_metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var plans map[string]TestPlan
	if err := json.Unmarshal(body, &plans); err != nil {
		t.Fatal(err)
	}
	p, ok := plans[name]
	if !ok {
		t.Fatalf("missing fixture %s", name)
	}
	return p
}

func TestPlanningMetadataBackendExamples(t *testing.T) {
	for _, tt := range []struct {
		name         string
		want, absent []string
	}{
		{"reached_cap", []string{"Estimated nodes needed: 2", "Node limit: 2 (not independently binding)", "Target time: 13s", "Estimated longest node: 13s (P90 durations; within target)"}, []string{"capped at", "Estimated longest node: 7s", "target exceeded"}},
		{"insufficient_history", []string{"insufficient timing history; target not used", "Node limit: 10 (not independently binding)", "Test selector limit: capped at 4", "Estimated longest node: 1s (P90 durations)"}, []string{"within target", "target exceeded", "Target time:", "Estimated nodes needed:"}},
		{"selected_share", []string{"Applied strategy: manual", "Selected: 1 of 4 test selectors (25%)", "Estimated compute: 5s of 14s (35.7%)", "Candidate timing coverage: 100%"}, []string{"Estimated compute: 5s of 14s (25%)", "P90"}},
		{"skipped_strategy", []string{"Attempted strategy: xgboost (skipped)", "Reason: no_model", "Selected: 9 of 9", "Estimated compute: unavailable"}, []string{"Applied strategy:"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, tt.name)
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p, nil)
			PrintSplitSummary(&buf, p)
			assertSummary(t, buf.String(), tt.want, tt.absent)
		})
	}
}

func TestSelectionDurationAvailability(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*SelectionMetadata)
		want   string
	}{
		{"zero selected", func(s *SelectionMetadata) { *s.DurationEstimates.SelectedTotalDurationMS = 0 }, "Estimated compute: 0ms of 14s (0%)"},
		{"full selection", func(s *SelectionMetadata) { *s.DurationEstimates.SelectedTotalDurationMS = 14000 }, "Estimated compute: 14s of 14s (100%)"},
		{"zero denominator", func(s *SelectionMetadata) {
			*s.DurationEstimates.CandidateTotalDurationMS = 0
			*s.DurationEstimates.SelectedTotalDurationMS = 0
		}, "Estimated compute: 0ms of 0ms (share unavailable)"},
		{"coverage threshold", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.5 }, "Estimated compute: 5s of 14s (35.7%)\n  Candidate timing coverage: 50%"},
		{"coverage rounding", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.57 }, "Estimated compute: 5s of 14s (35.7%)\n  Candidate timing coverage: 57%"},
		{"sparse coverage", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.49 }, "Estimated compute: unavailable"},
		{"missing coverage", func(s *SelectionMetadata) { s.DurationEstimates.CandidateTimingCoverage = nil }, "Estimated compute: unavailable"},
		{"missing selected", func(s *SelectionMetadata) { s.DurationEstimates.SelectedTotalDurationMS = nil }, "Estimated compute: unavailable"},
		{"missing candidate", func(s *SelectionMetadata) { s.DurationEstimates.CandidateTotalDurationMS = nil }, "Estimated compute: unavailable"},
		{"unknown estimator", func(s *SelectionMetadata) { s.DurationEstimates.Estimator = "future" }, "Estimated compute: unavailable"},
		{"selected pool estimator", func(s *SelectionMetadata) { s.DurationEstimates.Estimator = "mean_with_fallbacks_v1" }, "Estimated compute: unavailable"},
		{"absent estimates", func(s *SelectionMetadata) { s.DurationEstimates = nil }, "Estimated compute: unavailable"},
		{"skipped", func(s *SelectionMetadata) { *s.Applied = false }, "Estimated compute: unavailable"},
		{"unknown applied", func(s *SelectionMetadata) { s.Applied = nil }, "Estimated compute: unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, "selected_share")
			tt.change(p.Selection)
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p, nil)
			absent := []string{}
			if strings.HasSuffix(tt.want, "unavailable") {
				absent = append(absent, "Estimated compute: 5s")
			}
			if tt.name == "zero denominator" {
				absent = append(absent, "of 0ms (0%)")
			}
			assertSummary(t, buf.String(), []string{tt.want}, absent)
		})
	}
}

func TestSizingMetadataOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name         string
		change       func(*TestPlan)
		want, absent []string
	}{
		{"binding cap misses target", func(p *TestPlan) {
			*p.Sizing.MaxParallelismBinding = true
			*p.Sizing.TargetTimeMS = 8000
			*p.Sizing.EstimatedRequiredParallelism = 4
		}, []string{"Node limit: capped at 2", "Estimated nodes needed: 4", "target exceeded"}, []string{"within target"}},
		{"binding does not prove miss", func(p *TestPlan) { *p.Sizing.MaxParallelismBinding = true }, []string{"Node limit: capped at 2", "within target"}, []string{"target exceeded"}},
		{"tied caps", func(p *TestPlan) {
			p.Parallelism = 4
			*p.Settings.MaxParallelism = 4
			*p.Sizing.EstimatedRequiredParallelism = 8
		}, []string{"Estimated nodes needed: 8", "Node limit: 4 (not independently binding)"}, []string{"capped at", "Test selector limit:"}},
		{"uncapped miss", func(p *TestPlan) {
			p.Parallelism = 4
			*p.Settings.MaxParallelism = 10
			*p.Sizing.TargetTimeMS = 7000
			*p.Sizing.EstimatedMaxTaskDurationMS = 8000
		}, []string{"Node limit: 10 (not independently binding)", "Estimated longest node: 8s", "target exceeded"}, []string{"capped at"}},
		{"automatic target", func(p *TestPlan) {
			p.Settings.TargetTime = nil
			p.Sizing.TargetTimeSource = "longest_test"
			*p.Sizing.TargetTimeMS = 8000
		}, []string{"Target time: 8s (automatic, longest P90 test)"}, nil},
		{"fractional target", func(p *TestPlan) { *p.Sizing.TargetTimeMS = 12999.5 }, []string{"Target time: 12.9995s", "target exceeded"}, nil},
		{"absent flags", func(p *TestPlan) { p.Sizing.MaxParallelismBinding = nil; p.Sizing.RunnableUnitsBinding = nil }, []string{"Node limit: 2 (binding unknown)", "Test selector limit: binding unknown"}, []string{"not independently binding", "capped at"}},
		{"binding unknown maximum", func(p *TestPlan) { p.Settings = nil; *p.Sizing.MaxParallelismBinding = true }, []string{"Node limit: binding (maximum unavailable)"}, []string{"capped at"}},
		{"missing target", func(p *TestPlan) { p.Sizing.TargetTimeMS = nil }, []string{"Target time: unavailable"}, []string{"within target", "target exceeded"}},
		{"unknown target source", func(p *TestPlan) { p.Sizing.TargetTimeSource = "future" }, []string{"Target time: unavailable"}, []string{"within target", "target exceeded"}},
		{"unknown estimator", func(p *TestPlan) { p.Sizing.Estimator = "future" }, []string{"Estimated longest node: unavailable"}, []string{"within target", "target exceeded", "Estimated longest node: 7s"}},
		{"missing estimate", func(p *TestPlan) { p.Sizing.EstimatedMaxTaskDurationMS = nil }, []string{"Estimated longest node: unavailable"}, []string{"within target", "Estimated longest node: 7s"}},
		{"old plan", func(p *TestPlan) { p.Sizing = nil }, []string{"Sizing: unavailable", "Node limit: 2 (binding unknown)"}, []string{"capped at", "Estimated longest node:", "within target"}},
		{"unknown method", func(p *TestPlan) { p.Sizing.Method = "future" }, []string{"Sizing: unavailable (unknown method)"}, []string{"Estimated nodes needed:", "within target"}},
		{"fixed one", func(p *TestPlan) { p.Sizing = &SizingMetadata{Method: "fixed"} }, []string{"Sizing: fixed parallelism; target not used", "Estimated longest node: unavailable"}, []string{"Node limit:", "within target"}},
		{"single node", func(p *TestPlan) { p.Sizing = &SizingMetadata{Method: "single_node"} }, []string{"Sizing: single-node maximum; target not used"}, []string{"Node limit:", "within target"}},
		{"empty sparse plan", func(p *TestPlan) {
			p.Parallelism = 0
			p.Tasks = map[string]*Task{}
			p.Sizing.Method = "insufficient_history"
			*p.Sizing.RunnableUnits = 0
			*p.Sizing.EstimatedMaxTaskDurationMS = 0
		}, []string{"0 test selectors across 0 nodes", "Estimated longest node: 0ms"}, []string{"within target", "target exceeded"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, "reached_cap")
			tt.change(&p)
			var buf bytes.Buffer
			PrintSplitSummary(&buf, p)
			assertSummary(t, buf.String(), tt.want, tt.absent)
		})
	}
}

func TestSizingDecisionEstimator(t *testing.T) {
	for _, tt := range []struct {
		name, estimator, basis string
	}{
		{"mean decision", `"mean_with_fallbacks_v1"`, "mean durations"},
		{"P90 decision or fallback", `"p90_with_median_fallbacks_v1"`, "P90 durations"},
		{"missing decision basis", `null`, "basis unavailable"},
		{"unknown decision basis", `"future\n+++ forged"`, "unknown basis"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, "reached_cap")
			// Decode the new field independently of the allocation estimator.
			if err := json.Unmarshal([]byte(`{"target_time_estimator":`+tt.estimator+`,"target_time_ms":7999.5}`), p.Sizing); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			PrintSplitSummary(&buf, p)
			assertSummary(t, buf.String(), []string{
				"Estimated nodes needed: 2 (" + tt.basis + ")",
				"Target time: 7.9995s",
				"Estimated longest node: 13s (P90 durations; target exceeded)",
			}, []string{"mean estimate exceeds", "target unmet", "forged"})
		})
	}
}

func TestSizingUnusableTimings(t *testing.T) {
	var p TestPlan
	if err := json.Unmarshal([]byte(`{"parallelism":2,"settings":{"target_time":8,"max_parallelism":2},"sizing":{"method":"unusable_timings","runnable_units":4,"max_parallelism_binding":true,"runnable_units_binding":false,"estimator":"p90_with_median_fallbacks_v1","estimated_max_task_duration_ms":0}}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Sizing.TargetTimeMS != nil || p.Sizing.TargetTimeEstimator != nil || p.Sizing.EstimatedRequiredParallelism != nil || p.Sizing.TargetTimeSource != "" {
		t.Fatalf("unused target acquired metadata: %+v", p.Sizing)
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	assertSummary(t, buf.String(), []string{
		"Sizing: unusable timing estimates; target not used",
		"Node limit: capped at 2",
		"Estimated longest node: 0ms (P90 durations)",
	}, []string{"insufficient timing history", "Target time:", "Estimated nodes needed:", "within target", "target exceeded"})
}

func TestReturnedStrategyAvailability(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{`{"selection":{"strategy":"random","applied":true}}`, "Applied strategy: random"},
		{`{"selection":{"strategy":"rspec_changed_files","applied":false}}`, "Attempted strategy: rspec_changed_files"},
		{`{"selection":{"strategy":"austral","applied":null}}`, "Returned strategy: austral (applied status unavailable)"},
		{`{"selection":{"strategy":"future_strategy","applied":true}}`, "Applied strategy: future_strategy"},
		{`{"selection":{"strategy":"future_strategy","applied":false}}`, "Attempted strategy: future_strategy (skipped)"},
		{`{"selection":{"strategy":"future_strategy"}}`, "Returned strategy: future_strategy (applied status unavailable)"},
		{`{"selection":{"strategy":null,"applied":true}}`, "Applied (strategy unavailable)"},
		{`{"selection":{"strategy":null,"applied":false}}`, "Not applied (strategy unavailable)"},
		{`{"selection":{"strategy":null}}`, "Applied status: unavailable"},
		{`{"selection":{"strategy":"","applied":true}}`, "Applied (strategy unavailable)"},
		{`{"selection":{"strategy":"","applied":false}}`, "Not applied (strategy unavailable)"},
		{`{"selection":{"strategy":""}}`, "Applied status: unavailable"},
	} {
		var p TestPlan
		if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		PrintSelectionSummary(&buf, p, nil)
		assertSummary(t, buf.String(), []string{tt.want}, []string{"unsupported", "not recognised"})
	}
}

func TestReturnedSelectionTextSafety(t *testing.T) {
	for _, tt := range []struct{ name, value, want string }{
		{"controls", "future\n+++ forged\x1b[31m\u202e", `"future\n+++ forged\x1b[31m\u202e"`},
		{"invalid bytes", "future\xff", `"future\xff"`},
		{"multibyte across limit", strings.Repeat("界", 100), strings.Repeat("界", 66) + "…"},
		{"exact byte limit", strings.Repeat("界", 66) + "ab", strings.Repeat("界", 66) + "ab"},
		{"multibyte at limit", strings.Repeat("x", 200) + "界", strings.Repeat("x", 200) + "…"},
		{"invalid byte at limit", strings.Repeat("x", 199) + "\xffz", `"` + strings.Repeat("x", 199) + `\xff…"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := TestPlan{Selection: &SelectionMetadata{Strategy: &tt.value, SkippedReason: &tt.value}}
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p, nil)
			want := "Selection summary\n  Returned strategy: " + tt.want + " (applied status unavailable)\n  Reason: " + tt.want + "\n  Selected: unknown of unknown test selectors\n  Estimated compute: unavailable\n"
			if got := buf.String(); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestSelectionParameterComparison(t *testing.T) {
	for _, tt := range []struct {
		name, body   string
		requested    map[string]string
		want, absent []string
	}{
		{"equivalent numeric values", `{"score_cutoff":0.5,"count_cutoff":12,"proportion_cutoff":0,"duration_proportion_cutoff":0.5}`, map[string]string{"score_cutoff": "5e-1", "count_cutoff": "12", "proportion_cutoff": "0", "duration_proportion_cutoff": "0.50"}, nil, []string{"Returned parameter:"}},
		{"different or missing", `{"score_cutoff":0.5,"count_cutoff":12,"proportion_cutoff":0}`, map[string]string{"score_cutoff": "0.6", "count_cutoff": "10"}, []string{"score_cutoff = 0.5", "count_cutoff = 12", "proportion_cutoff = 0"}, nil},
		{"missing returned", `{}`, map[string]string{"score_cutoff": "0.5", "count_cutoff": "12"}, nil, []string{"Returned parameter:"}},
		{"invalid request is not a match", `{"score_cutoff":0,"count_cutoff":0}`, map[string]string{"score_cutoff": "invalid", "count_cutoff": "invalid"}, []string{"score_cutoff = 0", "count_cutoff = 0"}, nil},
		{"large integer stays exact", `{"count_cutoff":9007199254740993}`, map[string]string{"count_cutoff": "9007199254740992"}, []string{"count_cutoff = 9007199254740993"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s SelectionMetadata
			if err := json.Unmarshal([]byte(tt.body), &s); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, TestPlan{Selection: &s}, tt.requested)
			assertSummary(t, buf.String(), tt.want, tt.absent)
		})
	}
}

func TestSummaryValue(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"xgboost", "xgboost"}, {"0.5", "0.5"}, {"", `""`},
		{"has space", `"has space"`}, {"line\n\x1b", `"line\n\x1b"`},
		{"tab\tvalue", `"tab\tvalue"`}, {"quote\"", `"quote\""`},
		{"bad\xff", `"bad\xff"`}, {"invisible\u202e", `"invisible\u202e"`},
	} {
		if got := SummaryValue(tt.value); got != tt.want {
			t.Errorf("SummaryValue(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func assertSummary(t *testing.T, got string, want, absent []string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q:\n%s", s, got)
		}
	}
	for _, s := range append(absent, "+++", "\n\n\n", "runnable unit", "At maximum nodes", "Returned constraints") {
		if strings.Contains(got, s) {
			t.Errorf("unexpected %q:\n%s", s, got)
		}
	}
}
