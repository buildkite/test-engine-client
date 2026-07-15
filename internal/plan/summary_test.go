package plan

import (
	"bytes"
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
		"+++ Buildkite Test Engine Client: 📊 Split summary\n5 files across 2 nodes",
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
		"+++ Buildkite Test Engine Client: 📊 Split summary\n3 files across 2 nodes",
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

func TestPrintSplitSummary_SkipsWhenNoMetadata(t *testing.T) {
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a"}}},
		},
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	if buf.Len() != 0 {
		t.Errorf("expected no output, got: %s", buf.String())
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
		"+++ Buildkite Test Engine Client: 📊 Split summary\n2 examples across 2 nodes",
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
		"+++ Buildkite Test Engine Client: 📊 Split summary\n2 selectors across 2 nodes",
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
		"4 tests across 2 nodes",
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
		"4 tests across 2 nodes",
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

func TestPrintSplitSummary_SkipsFallback(t *testing.T) {
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
	if buf.Len() != 0 {
		t.Errorf("expected no output for fallback plan, got: %s", buf.String())
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
