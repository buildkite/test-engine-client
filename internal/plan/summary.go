package plan

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PrintSelectionSummary uses only returned metadata. In particular, neither
// task formats nor this invocation's flags establish the eligible denominator
// or the strategy used by an existing plan. requested only suppresses matching
// parameter lines; it never supplies missing returned metadata.
func PrintSelectionSummary(w io.Writer, p TestPlan, requested map[string]string) {
	fmt.Fprintln(w, "Selection summary")
	if p.Fallback {
		if len(p.Tasks) == 0 {
			fmt.Fprintln(w, "  Not determined: taskless fallback placeholder.")
		} else {
			fmt.Fprintln(w, "  Not applied: local fallback uses the full locally discovered suite.")
		}
		return
	}
	s := p.Selection
	if s == nil {
		fmt.Fprintln(w, "  No selection metadata returned")
		return
	}
	strategy := ""
	if s.Strategy != nil && *s.Strategy != "" {
		strategy = boundedSelectionText(*s.Strategy)
	}
	switch {
	case strategy == "" && s.Applied == nil:
		fmt.Fprintln(w, "  Applied status: unavailable")
	case strategy == "" && *s.Applied:
		fmt.Fprintln(w, "  Applied (strategy unavailable)")
	case strategy == "":
		fmt.Fprintln(w, "  Not applied (strategy unavailable)")
	case s.Applied == nil:
		fmt.Fprintf(w, "  Returned strategy: %s (applied status unavailable)\n", strategy)
	case *s.Applied:
		fmt.Fprintf(w, "  Applied strategy: %s\n", strategy)
	default:
		fmt.Fprintf(w, "  Attempted strategy: %s (skipped)\n", strategy)
	}
	if s.SkippedReason != nil {
		fmt.Fprintf(w, "  Reason: %s\n", boundedSelectionText(*s.SkippedReason))
	}
	if s.SelectedCount != nil && s.CandidateCount != nil {
		share := "percentage unavailable"
		if *s.CandidateCount > 0 {
			share = percentOf(*s.SelectedCount, *s.CandidateCount)
		}
		fmt.Fprintf(w, "  Selected: %d of %d test selectors (%s)\n", *s.SelectedCount, *s.CandidateCount, share)
	} else {
		selected, eligible := "unknown", "unknown"
		if s.SelectedCount != nil {
			selected = strconv.Itoa(*s.SelectedCount)
		}
		if s.CandidateCount != nil {
			eligible = strconv.Itoa(*s.CandidateCount)
		}
		fmt.Fprintf(w, "  Selected: %s of %s test selectors\n", selected, eligible)
	}
	for _, param := range []struct {
		name  string
		value any
	}{
		{"score_cutoff", s.ScoreCutoff},
		{"count_cutoff", s.CountCutoff},
		{"proportion_cutoff", s.ProportionCutoff},
		{"duration_proportion_cutoff", s.DurationProportionCutoff},
	} {
		var value string
		matches := false
		switch v := param.value.(type) {
		case *float64:
			if v != nil {
				value = strconv.FormatFloat(*v, 'g', -1, 64)
				invocation, err := strconv.ParseFloat(requested[param.name], 64)
				matches = err == nil && *v == invocation
			}
		case *int:
			if v != nil {
				value = strconv.Itoa(*v)
				invocation, err := strconv.Atoi(requested[param.name])
				matches = err == nil && *v == invocation
			}
		}
		if value != "" && !matches {
			fmt.Fprintf(w, "  Returned parameter: %s = %s\n", param.name, value)
		}
	}
	if s.EffectiveCount != nil {
		fmt.Fprintf(w, "  Effective count: %d\n", *s.EffectiveCount)
	}
	printSelectionDurationSummary(w, s)
}

func printSelectionDurationSummary(w io.Writer, s *SelectionMetadata) {
	d := s.DurationEstimates
	if s.Applied == nil || !*s.Applied || d == nil ||
		d.Estimator != "mean_with_candidate_median_fallbacks_v1" ||
		d.CandidateTotalDurationMS == nil || d.SelectedTotalDurationMS == nil ||
		d.CandidateTimingCoverage == nil || *d.CandidateTimingCoverage < 0.5 {
		fmt.Fprintln(w, "  Estimated compute: unavailable")
		return
	}
	share := "share unavailable"
	if *d.CandidateTotalDurationMS > 0 {
		share = percentOf(*d.SelectedTotalDurationMS, *d.CandidateTotalDurationMS)
	}
	fmt.Fprintf(w, "  Estimated compute: %s of %s (%s)\n",
		planningDurationMS(float64(*d.SelectedTotalDurationMS)), planningDurationMS(float64(*d.CandidateTotalDurationMS)), share)
	fmt.Fprintf(w, "  Candidate timing coverage: %.0f%%\n", *d.CandidateTimingCoverage*100)
}

// PrintSplitSummary writes a human-readable summary of the resolved test plan
// to w (typically os.Stderr). At parallelism > 1 it uses per-format
// TimingMetadata to break down known vs unknown cases. At parallelism == 1 the
// server skips the per-format timing fetch and emits an empty TimingMetadata,
// so there is no history breakdown. Missing metadata is not evidence of no history.
func PrintSplitSummary(w io.Writer, p TestPlan) {
	if p.Fallback && len(p.Tasks) == 0 {
		fmt.Fprintln(w, "Split summary")
		fmt.Fprintf(w, "  Placeholder only: %d %s; no test allocation computed.\n", p.Parallelism, pluralize(p.Parallelism, "node"))
		return
	}
	fileTotal, fileKnown := countByFormat(p, TestCaseFormatFile)
	exampleTotal, exampleKnown := countByFormat(p, TestCaseFormatExample)
	selectorTotal, selectorKnown := countByFormat(p, TestCaseFormatSelector)

	total := fileTotal + exampleTotal + selectorTotal
	nodes := p.Parallelism
	mixed := countNonZero(fileTotal, exampleTotal, selectorTotal) > 1
	noun := summaryNoun(fileTotal, exampleTotal, selectorTotal)
	if p.Fallback && p.Tasks != nil {
		// Run's local splitter populates tasks but not Parallelism or Format.
		nodes = len(p.Tasks)
		noun = "test selector"
	}

	fmt.Fprintln(w, "Split summary")
	if p.Tasks == nil {
		fmt.Fprintf(w, "  %d %s (test selector count unavailable)\n", nodes, pluralize(nodes, "node"))
	} else {
		fmt.Fprintf(w, "  %d %s across %d %s\n",
			total, pluralize(total, noun), nodes, pluralize(nodes, "node"))
	}
	if p.Fallback {
		fmt.Fprintln(w, "  Local non-intelligent split; timing estimates unavailable.")
		return
	}
	printSizingSummary(w, p)
	if p.TimingMetadata == nil {
		fmt.Fprintln(w, "  Historical timings: unavailable")
		return
	}

	// At parallelism == 1 the server skips the per-format timing fetch and
	// emits an empty TimingMetadata, so there is no breakdown to print.
	if p.TimingMetadata.File == nil && p.TimingMetadata.Example == nil && p.TimingMetadata.Selector == nil {
		return
	}

	if fileTotal > 0 {
		printFormatBreakdown(w, fileTotal, fileKnown, "file", p.TimingMetadata.File, mixed)
	}
	if exampleTotal > 0 {
		printFormatBreakdown(w, exampleTotal, exampleKnown, "example", p.TimingMetadata.Example, mixed)
	}
	if selectorTotal > 0 {
		printFormatBreakdown(w, selectorTotal, selectorKnown, "selector", p.TimingMetadata.Selector, mixed)
	}
}

func printSizingSummary(w io.Writer, p TestPlan) {
	s := p.Sizing
	if s == nil {
		fmt.Fprintln(w, "  Sizing: unavailable")
		if p.Settings != nil {
			if p.Settings.TargetTime != nil {
				fmt.Fprintf(w, "  Target time: %gs (usage unknown)\n", *p.Settings.TargetTime)
			}
			printNodeLimit(w, p.Settings.MaxParallelism, nil)
		}
		return
	}
	// Configured targets are not necessarily used by the sizing branch.
	hasTarget := s.Method == "timing" && s.TargetTimeMS != nil &&
		(s.TargetTimeSource == "explicit" || s.TargetTimeSource == "longest_test")
	switch s.Method {
	case "fixed":
		fmt.Fprintln(w, "  Sizing: fixed parallelism; target not used")
	case "single_node":
		fmt.Fprintln(w, "  Sizing: single-node maximum; target not used")
	case "insufficient_history":
		fmt.Fprintln(w, "  Sizing: insufficient timing history; target not used")
	case "unusable_timings":
		fmt.Fprintln(w, "  Sizing: unusable timing estimates; target not used")
	case "timing":
		if hasTarget {
			suffix := ""
			if s.TargetTimeSource == "longest_test" {
				suffix = " (automatic, longest P90 test)"
			}
			fmt.Fprintf(w, "  Target time: %s%s\n", planningDurationMS(*s.TargetTimeMS), suffix)
		} else {
			fmt.Fprintln(w, "  Target time: unavailable")
		}
		if s.EstimatedRequiredParallelism != nil {
			basis := "basis unavailable"
			if s.TargetTimeEstimator != nil {
				switch *s.TargetTimeEstimator {
				case "p90_with_median_fallbacks_v1":
					basis = "P90 durations"
				case "mean_with_fallbacks_v1":
					basis = "mean durations"
				default:
					basis = "unknown basis"
				}
			}
			fmt.Fprintf(w, "  Estimated nodes needed: %d (%s)\n", *s.EstimatedRequiredParallelism, basis)
		} else {
			fmt.Fprintln(w, "  Estimated nodes needed: unavailable")
		}
	default:
		fmt.Fprintln(w, "  Sizing: unavailable (unknown method)")
		return
	}
	if s.Method == "timing" || s.Method == "insufficient_history" || s.Method == "unusable_timings" {
		var maximum *int
		if p.Settings != nil {
			maximum = p.Settings.MaxParallelism
		}
		printNodeLimit(w, maximum, s.MaxParallelismBinding)
		if s.RunnableUnitsBinding == nil {
			fmt.Fprintln(w, "  Test selector limit: binding unknown")
		} else if *s.RunnableUnitsBinding {
			if s.RunnableUnits != nil {
				fmt.Fprintf(w, "  Test selector limit: capped at %d\n", *s.RunnableUnits)
			} else {
				fmt.Fprintln(w, "  Test selector limit: binding (count unavailable)")
			}
		}
	}
	if s.Estimator != "p90_with_median_fallbacks_v1" || s.EstimatedMaxTaskDurationMS == nil {
		fmt.Fprintln(w, "  Estimated longest node: unavailable")
		return
	}
	fmt.Fprintf(w, "  Estimated longest node: %s (P90 durations", planningDurationMS(float64(*s.EstimatedMaxTaskDurationMS)))
	if hasTarget {
		if float64(*s.EstimatedMaxTaskDurationMS) > *s.TargetTimeMS {
			fmt.Fprint(w, "; target exceeded")
		} else {
			fmt.Fprint(w, "; within target")
		}
	}
	fmt.Fprintln(w, ")")
}

func printNodeLimit(w io.Writer, maximum *int, binding *bool) {
	limit := "unavailable"
	if maximum != nil {
		limit = strconv.Itoa(*maximum)
	}
	switch {
	case binding == nil:
		fmt.Fprintf(w, "  Node limit: %s (binding unknown)\n", limit)
	case *binding:
		if maximum == nil {
			fmt.Fprintln(w, "  Node limit: binding (maximum unavailable)")
		} else {
			fmt.Fprintf(w, "  Node limit: capped at %s\n", limit)
		}
	default:
		fmt.Fprintf(w, "  Node limit: %s (not independently binding)\n", limit)
	}
}

// SummaryValue leaves ordinary values readable while escaping whitespace,
// control characters and quotes so a value cannot forge another log line.
func SummaryValue(value string) string {
	if value == "" || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || !unicode.IsPrint(r) || r == '"' || r == '\\'
	}) >= 0 {
		return strconv.Quote(value)
	}
	return value
}

// Bound returned text to 200 bytes before escaping, without splitting a UTF-8
// sequence. Invalid input bytes remain intact so SummaryValue can escape them.
func boundedSelectionText(value string) string {
	if len(value) > 200 {
		end := 0
		for i := range value {
			if i > 200 {
				break
			}
			end = i
		}
		value = value[:end] + "…"
	}
	return SummaryValue(value)
}

// Keep fractional sizing targets visible so a near-boundary comparison does
// not display equal rounded durations while reporting an exceeded target.
func planningDurationMS(ms float64) string {
	if ms < 1000 {
		return strconv.FormatFloat(ms, 'f', -1, 64) + "ms"
	}
	return strconv.FormatFloat(ms/1000, 'f', -1, 64) + "s"
}

// HasNoSelectorTimingHistory reports whether a multi-node selector plan used
// default durations because no historical selector timings were available.
func (p TestPlan) HasNoSelectorTimingHistory() bool {
	if p.Fallback || p.Parallelism <= 1 || p.TimingMetadata == nil ||
		p.TimingMetadata.Selector == nil || p.TimingMetadata.Selector.MedianDuration != nil {
		return false
	}

	selectorTotal, selectorKnown := countByFormat(p, TestCaseFormatSelector)
	return selectorTotal > 0 && selectorKnown == 0
}

// countByFormat returns (total, known) for cases of the given format. The
// empty (default) Format value is treated as TestCaseFormatFile.
func countByFormat(p TestPlan, format TestCaseFormat) (total, known int) {
	for _, task := range p.Tasks {
		for _, tc := range task.Tests {
			f := tc.Format
			if f == "" {
				f = TestCaseFormatFile
			}
			if f != format {
				continue
			}
			total++
			if tc.TimingSampleSize > 0 {
				known++
			}
		}
	}
	return total, known
}

func countNonZero(values ...int) int {
	count := 0
	for _, value := range values {
		if value > 0 {
			count++
		}
	}
	return count
}

// summaryNoun returns the singular heading noun. The plan-level summary uses
// "file"/"example"/"selector" when the plan only contains one format, or
// "test selector" when multiple formats are present. Callers pluralize as needed.
func summaryNoun(fileTotal, exampleTotal, selectorTotal int) string {
	switch {
	case fileTotal+exampleTotal+selectorTotal == 0:
		return "test selector"
	case exampleTotal == 0 && selectorTotal == 0:
		return "file"
	case fileTotal == 0 && selectorTotal == 0:
		return "example"
	case fileTotal == 0 && exampleTotal == 0:
		return "selector"
	default:
		return "test selector"
	}
}

// pluralize returns singular when n == 1, otherwise singular + "s".
func pluralize(n int, singular string) string {
	if n == 1 {
		return singular
	}
	return singular + "s"
}

// printFormatBreakdown writes the per-format lines. noun is the singular form
// ("file" or "example"); each line is pluralized to match its own count.
func printFormatBreakdown(w io.Writer, total, known int, noun string, meta *FormatTimingMetadata, mixed bool) {
	indent := "  "
	if mixed {
		fmt.Fprintf(w, "  %d %s\n", total, pluralize(total, noun))
		indent = "    "
	}
	fmt.Fprintf(w, "%sHistorical timings: %d of %d %s (%s)\n", indent, known, total, pluralize(total, noun), percentOf(known, total))
	unknown := total - known
	if unknown > 0 {
		suffix := ""
		if meta != nil {
			if known == 0 {
				suffix = fmt.Sprintf("; default duration %s", formatDurationMS(meta.DefaultDuration))
			} else if meta.MedianDuration != nil {
				suffix = fmt.Sprintf("; median duration %s", formatDurationMS(*meta.MedianDuration))
			}
		}
		fmt.Fprintf(w, "%sNo history: %d %s (%s)%s\n", indent, unknown, pluralize(unknown, noun), percentOf(unknown, total), suffix)
	}
}

func percentOf(n, total int) string {
	if total == 0 {
		return "0%"
	}
	percent := float64(n) / float64(total) * 100
	rounded := math.Round(percent*10) / 10
	if rounded == 0 && n > 0 {
		return "<0.1%"
	}
	if rounded == 100 && n < total {
		return ">99.9%"
	}
	if rounded == math.Trunc(rounded) {
		return fmt.Sprintf("%.0f%%", rounded)
	}
	return fmt.Sprintf("%.1f%%", rounded)
}

func formatDurationMS(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", int(ms+0.5))
	}
	return fmt.Sprintf("%.1fs", ms/1000.0)
}
