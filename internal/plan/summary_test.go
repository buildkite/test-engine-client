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
		"3 files (60%) estimated from past historical durations",
		"2 files (40%) had no history — assumed median (4.2s)",
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
		"3 files (100%) had no history and used the default duration (1.0s)",
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

	if strings.Contains(got, "assumed median") {
		t.Errorf("expected no median when MedianDuration is nil, got:\n%s", got)
	}
	if !strings.Contains(got, "had no history\n") {
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
	if !strings.Contains(buf.String(), "1 file across 1 node") || !strings.Contains(buf.String(), "Timing history unavailable.") {
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
	if strings.Contains(got, "estimated from past") || strings.Contains(got, "had no history") {
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
		"1 example (50%) estimated from past historical durations",
		"1 example (50%) had no history",
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
		"1 selector (50%) estimated from past historical durations",
		"1 selector (50%) had no history — assumed median (1.8s)",
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
		"2 selectors (100%) had no history and used the default duration (1.0s)",
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
		"4 runnable units across 2 nodes",
		"  2 files\n",
		"    1 (50%) estimated from past historical durations",
		"    1 (50%) had no history — assumed median (4.2s)",
		"  2 examples\n",
		"    1 (50%) estimated from past historical durations",
		"    1 (50%) had no history — assumed median (150ms)",
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
		"4 runnable units across 2 nodes",
		"  1 file\n",
		"    1 (100%) estimated from past historical durations",
		"  1 example\n",
		"    1 (100%) had no history and used the default duration (500ms)",
		"  2 selectors\n",
		"    1 (50%) estimated from past historical durations",
		"    1 (50%) had no history — assumed median (1.8s)",
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
	if !strings.Contains(buf.String(), "1 runnable unit across 1 node") || !strings.Contains(buf.String(), "Local non-intelligent split") {
		t.Errorf("expected local fallback summary, got: %s", buf.String())
	}
	if strings.Contains(buf.String(), "had no history") {
		t.Errorf("unexpected server timing breakdown for fallback: %s", buf.String())
	}
}

func TestPrintSelectionSummary(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"old plan", `{}`, "Outcome unknown (selection metadata unavailable)."},
		{"null", `{"selection":null}`, "Outcome unknown (selection metadata unavailable)."},
		{"missing applied", `{"selection":{"applied":null,"selected_count":0}}`, "Outcome unknown (applied status unavailable).\n  Selected: 0; eligible runnable units: unknown."},
		{"zero selected", `{"selection":{"applied":true,"candidate_count":5,"selected_count":0}}`, "Applied.\n  Selected 0 of 5 eligible runnable units (0%)."},
		{"zero eligible", `{"selection":{"applied":true,"candidate_count":0,"selected_count":0}}`, "Selected 0 of 0 eligible runnable units (percentage unavailable: no eligible units)."},
		{"full selection", `{"selection":{"applied":true,"candidate_count":5,"selected_count":5}}`, "Selected 5 of 5 eligible runnable units (100%)."},
		{"mixed denominator", `{"selection":{"applied":true,"candidate_count":6,"selected_count":2},"tasks":{"0":{"tests":[{"format":"file"},{"format":"example"}]}}}`, "Selected 2 of 6 eligible runnable units (33.3%)."},
		{"fallback reason", `{"selection":{"applied":false,"candidate_count":5,"selected_count":5,"skipped_reason":"no_model"}}`, "Not applied.\n  Reason: \"no_model\"\n  Selected 5 of 5 eligible runnable units (100%)."},
		{"zero cutoffs", `{"selection":{"score_cutoff":0,"count_cutoff":0,"proportion_cutoff":0,"duration_proportion_cutoff":0,"effective_count":0}}`, "Returned parameters: score_cutoff=0, count_cutoff=0, proportion_cutoff=0, duration_proportion_cutoff=0, effective_count=0"},
		{"null cutoffs", `{"selection":{"score_cutoff":null,"count_cutoff":null}}`, "Selected: unknown; eligible runnable units: unknown."},
		{"local fallback", `{"Fallback":true}`, "Not applied: local fallback uses the full locally discovered suite."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var p TestPlan
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("want %q, got:\n%s", tt.want, &buf)
			}
			if strings.Contains(buf.String(), "+++") || strings.Contains(buf.String(), "\n\n") {
				t.Errorf("redundant group or blank line: %q", buf.String())
			}
		})
	}
}

func TestSelectionReasonBoundedAndEscaped(t *testing.T) {
	reason := "no_model\n+++ forged\x1b" + strings.Repeat("x", 1000)
	p := TestPlan{Selection: &SelectionMetadata{SkippedReason: &reason}}
	var buf bytes.Buffer
	PrintSelectionSummary(&buf, p)
	if !strings.Contains(buf.String(), `no_model\n+++ forged\x1b`) || len(buf.String()) > 400 {
		t.Fatalf("reason not safely bounded: %q", buf.String())
	}
}

func TestPrintSplitSummary_ReturnedConstraints(t *testing.T) {
	for _, tt := range []struct {
		body, want string
		atCap      bool
	}{
		{`{"parallelism":2,"settings":{"target_time":120.5,"max_parallelism":2}}`, "target time 120.5s; maximum nodes 2", true},
		{`{"parallelism":2,"settings":{"max_parallelism":5}}`, "target time unknown; maximum nodes 5", false},
		{`{"parallelism":0,"tasks":{},"settings":{"target_time":0,"max_parallelism":0}}`, "target time 0s; maximum nodes 0", false},
		{`{"parallelism":1,"settings":{"target_time":null,"max_parallelism":null}}`, "target time unknown; maximum nodes unknown", false},
	} {
		var p TestPlan
		if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		PrintSplitSummary(&buf, p)
		if !strings.Contains(buf.String(), tt.want) || strings.Contains(buf.String(), "At maximum nodes") != tt.atCap {
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
		{"reached_cap", []string{"At maximum nodes.", "estimated need 2 nodes before caps", "Sizing runnable units: 4", "maximum nodes not independently binding; runnable units not independently binding", "Sizing target: 13s (explicit)", "Estimated longest node: 13s (P90 packing", "within sizing target", "not a runtime guarantee"}, []string{"Estimated longest node: 7s", "exceeds sizing target"}},
		{"insufficient_history", []string{"target time 13s", "insufficient timing history; target time not used", "maximum nodes not independently binding; runnable units binding", "Estimated longest node: 1s (P90 packing"}, []string{"within sizing target", "exceeds sizing target", "Sizing target:", "estimated need"}},
		{"selected_share", []string{"Applied strategy: manual", "Selected 1 of 4 eligible runnable units (25%)", "selected 5s of 14s eligible candidate total (mean with candidate median/default fallbacks)", "Estimated duration share: 35.7%; candidate timing coverage: 100%", "Compute, not wall time or the requested cutoff"}, []string{"Estimated duration share: 25%", "P90"}},
		{"skipped_strategy", []string{"Attempted strategy: xgboost (skipped; not applied)", "Reason: \"no_model\"", "Selected 9 of 9", "Estimated duration share: unavailable"}, []string{"Applied strategy:"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, tt.name)
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p)
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
		{"zero selected", func(s *SelectionMetadata) { *s.DurationEstimates.SelectedTotalDurationMS = 0 }, "Estimated duration share: 0%;"},
		{"full selection", func(s *SelectionMetadata) { *s.DurationEstimates.SelectedTotalDurationMS = 14000 }, "Estimated duration share: 100%;"},
		{"zero denominator", func(s *SelectionMetadata) {
			*s.DurationEstimates.CandidateTotalDurationMS = 0
			*s.DurationEstimates.SelectedTotalDurationMS = 0
		}, "selected 0ms of 0ms eligible candidate total"},
		{"coverage threshold", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.5 }, "Estimated duration share: 35.7%; candidate timing coverage: 50%"},
		{"coverage rounding", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.57 }, "Estimated duration share: 35.7%; candidate timing coverage: 57%"},
		{"sparse coverage", func(s *SelectionMetadata) { *s.DurationEstimates.CandidateTimingCoverage = 0.49 }, "Estimated duration share: unavailable."},
		{"missing coverage", func(s *SelectionMetadata) { s.DurationEstimates.CandidateTimingCoverage = nil }, "Estimated duration share: unavailable."},
		{"missing selected", func(s *SelectionMetadata) { s.DurationEstimates.SelectedTotalDurationMS = nil }, "Estimated duration share: unavailable."},
		{"missing candidate", func(s *SelectionMetadata) { s.DurationEstimates.CandidateTotalDurationMS = nil }, "Estimated duration share: unavailable."},
		{"unknown estimator", func(s *SelectionMetadata) { s.DurationEstimates.Estimator = "future" }, "Estimated duration share: unavailable."},
		{"selected pool estimator", func(s *SelectionMetadata) { s.DurationEstimates.Estimator = "mean_with_fallbacks_v1" }, "Estimated duration share: unavailable."},
		{"absent estimates", func(s *SelectionMetadata) { s.DurationEstimates = nil }, "Estimated duration share: unavailable."},
		{"skipped", func(s *SelectionMetadata) { *s.Applied = false }, "Estimated duration share: unavailable."},
		{"unknown applied", func(s *SelectionMetadata) { s.Applied = nil }, "Estimated duration share: unavailable."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := planningMetadataFixture(t, "selected_share")
			tt.change(p.Selection)
			var buf bytes.Buffer
			PrintSelectionSummary(&buf, p)
			absent := []string{}
			if strings.HasSuffix(tt.want, "unavailable.") {
				absent = append(absent, "Estimated cumulative compute:")
			}
			if tt.name == "zero denominator" {
				absent = append(absent, "Estimated duration share: 0%")
				assertSummary(t, buf.String(), []string{"Estimated duration share: unavailable (zero candidate total)"}, nil)
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
		}, []string{"maximum nodes binding;", "estimated need 4 nodes before caps", "exceeds sizing target"}, []string{"within sizing target"}},
		{"binding does not prove miss", func(p *TestPlan) { *p.Sizing.MaxParallelismBinding = true }, []string{"maximum nodes binding;", "within sizing target"}, []string{"exceeds sizing target"}},
		{"tied caps", func(p *TestPlan) {
			p.Parallelism = 4
			*p.Settings.MaxParallelism = 4
			*p.Sizing.EstimatedRequiredParallelism = 8
		}, []string{"At maximum nodes.", "estimated need 8 nodes before caps", "maximum nodes not independently binding; runnable units not independently binding"}, nil},
		{"uncapped miss", func(p *TestPlan) {
			p.Parallelism = 4
			*p.Settings.MaxParallelism = 10
			*p.Sizing.TargetTimeMS = 7000
			*p.Sizing.EstimatedMaxTaskDurationMS = 8000
		}, []string{"maximum nodes not independently binding", "Estimated longest node: 8s", "exceeds sizing target"}, []string{"At maximum nodes"}},
		{"automatic target", func(p *TestPlan) {
			p.Settings.TargetTime = nil
			p.Sizing.TargetTimeSource = "longest_test"
			*p.Sizing.TargetTimeMS = 8000
		}, []string{"Sizing target: 8s (automatic: longest test P90 estimate)"}, nil},
		{"fractional target", func(p *TestPlan) { *p.Sizing.TargetTimeMS = 12999.5 }, []string{"Sizing target: 12.9995s", "exceeds sizing target"}, nil},
		{"absent flags", func(p *TestPlan) { p.Sizing.MaxParallelismBinding = nil; p.Sizing.RunnableUnitsBinding = nil }, []string{"maximum nodes binding unknown; runnable units binding unknown"}, []string{"not independently binding"}},
		{"missing target", func(p *TestPlan) { p.Sizing.TargetTimeMS = nil }, []string{"Sizing target: unavailable"}, []string{"within sizing target", "exceeds sizing target"}},
		{"unknown target source", func(p *TestPlan) { p.Sizing.TargetTimeSource = "future" }, []string{"Sizing target: unavailable"}, []string{"within sizing target", "exceeds sizing target"}},
		{"unknown estimator", func(p *TestPlan) { p.Sizing.Estimator = "future" }, []string{"Estimated longest node: unavailable"}, []string{"within sizing target", "exceeds sizing target", "Estimated longest node: 7s"}},
		{"missing estimate", func(p *TestPlan) { p.Sizing.EstimatedMaxTaskDurationMS = nil }, []string{"Estimated longest node: unavailable"}, []string{"within sizing target", "Estimated longest node: 7s"}},
		{"old plan", func(p *TestPlan) { p.Sizing = nil }, []string{"does not establish that the cap limited sizing"}, []string{"Sizing:", "Estimated longest node:", "within sizing target"}},
		{"unknown method", func(p *TestPlan) { p.Sizing.Method = "future" }, []string{"Sizing: unavailable (unknown method)"}, []string{"timing-based", "within sizing target"}},
		{"fixed one", func(p *TestPlan) { p.Sizing = &SizingMetadata{Method: "fixed"} }, []string{"Sizing: fixed parallelism; no timing target used", "Estimated longest node: unavailable"}, []string{"Caps:", "within sizing target"}},
		{"single node", func(p *TestPlan) { p.Sizing = &SizingMetadata{Method: "single_node"} }, []string{"Sizing: single-node maximum; no timing target used"}, []string{"Caps:", "within sizing target"}},
		{"empty sparse plan", func(p *TestPlan) {
			p.Parallelism = 0
			p.Tasks = map[string]*Task{}
			p.Sizing.Method = "insufficient_history"
			*p.Sizing.RunnableUnits = 0
			*p.Sizing.EstimatedMaxTaskDurationMS = 0
		}, []string{"0 runnable units across 0 nodes", "Sizing runnable units: 0", "Estimated longest node: 0ms"}, []string{"within sizing target", "exceeds sizing target"}},
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
		{"mean decision", `"mean_with_fallbacks_v1"`, "mean with median/default fallbacks"},
		{"P90 decision or fallback", `"p90_with_median_fallbacks_v1"`, "P90 with median/default fallbacks"},
		{"missing decision basis", `null`, "unavailable"},
		{"unknown decision basis", `"future\n+++ forged"`, "unknown"},
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
				"estimated need 2 nodes before caps (decision basis: " + tt.basis + ")",
				"Sizing target: 7.9995s (explicit)",
				"Estimated longest node: 13s (P90 packing with median/default fallbacks); P90 estimate exceeds sizing target",
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
		"target time 8s; maximum nodes 2",
		"Sizing: unusable timing estimates; target time not used.",
		"Sizing runnable units: 4",
		"Caps: maximum nodes binding; runnable units not independently binding",
		"Estimated longest node: 0ms (P90 packing",
	}, []string{"insufficient timing history", "Sizing target:", "decision basis", "estimated need", "within sizing target", "exceeds sizing target"})
}

func TestReturnedStrategyAvailability(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{`{"selection":{"strategy":"random","applied":true}}`, "Applied strategy: random"},
		{`{"selection":{"strategy":"rspec_changed_files","applied":false}}`, "Attempted strategy: rspec_changed_files"},
		{`{"selection":{"strategy":"austral","applied":null}}`, "Returned strategy: austral (applied status unavailable)"},
		{`{"selection":{"strategy":"future\n+++ forged","applied":true}}`, "Returned strategy: unavailable (unsupported)"},
		{`{"selection":{"strategy":null,"applied":true}}`, "Applied."},
	} {
		var p TestPlan
		if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		PrintSelectionSummary(&buf, p)
		assertSummary(t, buf.String(), []string{tt.want}, []string{"forged"})
	}
}

func assertSummary(t *testing.T, got string, want, absent []string) {
	t.Helper()
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q:\n%s", s, got)
		}
	}
	for _, s := range append(absent, "+++", "\n\n") {
		if strings.Contains(got, s) {
			t.Errorf("unexpected %q:\n%s", s, got)
		}
	}
}
