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
		"2 files (40%) had no history",
		"4.2s",
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

func TestPrintSplitSummary_ParallelismOneUsesKnownRatio(t *testing.T) {
	// At parallelism=1 the server skips per-format timing metadata, so the
	// summary derives counts from the plan-level known_timings_ratio.
	ratio := 0.75
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{
				{Path: "a"}, {Path: "b"}, {Path: "c"}, {Path: "d"},
			}},
		},
		TimingMetadata:    &TimingMetadata{},
		KnownTimingsRatio: &ratio,
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	for _, want := range []string{
		"4 files across 1 nodes",
		"3 files (75%) estimated from past historical durations",
		"1 files (25%) had no history",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestPrintSplitSummary_ParallelismOneZeroRatio(t *testing.T) {
	ratio := 0.0
	p := TestPlan{
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a"}, {Path: "b"}}},
		},
		TimingMetadata:    &TimingMetadata{},
		KnownTimingsRatio: &ratio,
	}
	var buf bytes.Buffer
	PrintSplitSummary(&buf, p)
	got := buf.String()

	if !strings.Contains(got, "2 files (100%) had no history") {
		t.Errorf("expected all-no-history line, got:\n%s", got)
	}
	if strings.Contains(got, "estimated from past") {
		t.Errorf("unexpected estimated line at ratio=0:\n%s", got)
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
		"1 examples (50%) estimated from past historical durations",
		"1 examples (50%) had no history",
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
