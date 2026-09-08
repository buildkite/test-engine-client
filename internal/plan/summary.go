package plan

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// PrintSelectionSummary uses only returned metadata. In particular, neither
// task formats nor this invocation's flags establish the eligible denominator
// or the strategy used by an existing plan.
func PrintSelectionSummary(w io.Writer, p TestPlan) {
	fmt.Fprintln(w, "Selection summary")
	if p.Fallback {
		fmt.Fprintln(w, "  Not applied: local fallback uses the full locally discovered suite.")
		return
	}
	s := p.Selection
	if s == nil {
		fmt.Fprintln(w, "  Outcome unknown (selection metadata unavailable).")
		return
	}
	switch {
	case s.Applied == nil:
		fmt.Fprintln(w, "  Outcome unknown (applied status unavailable).")
	case *s.Applied:
		fmt.Fprintln(w, "  Applied.")
	default:
		fmt.Fprintln(w, "  Not applied.")
	}
	if s.SkippedReason != nil {
		reason := *s.SkippedReason
		if len(reason) > 200 {
			reason = reason[:200] + "…"
		}
		fmt.Fprintf(w, "  Reason: %q\n", reason)
	}
	if s.SelectedCount != nil && s.CandidateCount != nil {
		share := "percentage unavailable: no eligible units"
		if *s.CandidateCount > 0 {
			share = percentOf(*s.SelectedCount, *s.CandidateCount)
		}
		fmt.Fprintf(w, "  Selected %d of %d eligible runnable units (%s).\n", *s.SelectedCount, *s.CandidateCount, share)
	} else {
		selected, eligible := "unknown", "unknown"
		if s.SelectedCount != nil {
			selected = strconv.Itoa(*s.SelectedCount)
		}
		if s.CandidateCount != nil {
			eligible = strconv.Itoa(*s.CandidateCount)
		}
		fmt.Fprintf(w, "  Selected: %s; eligible runnable units: %s.\n", selected, eligible)
	}
	var params []string
	if s.ScoreCutoff != nil {
		params = append(params, fmt.Sprintf("score_cutoff=%g", *s.ScoreCutoff))
	}
	if s.CountCutoff != nil {
		params = append(params, fmt.Sprintf("count_cutoff=%d", *s.CountCutoff))
	}
	if s.ProportionCutoff != nil {
		params = append(params, fmt.Sprintf("proportion_cutoff=%g", *s.ProportionCutoff))
	}
	if s.DurationProportionCutoff != nil {
		params = append(params, fmt.Sprintf("duration_proportion_cutoff=%g", *s.DurationProportionCutoff))
	}
	if s.EffectiveCount != nil {
		params = append(params, fmt.Sprintf("effective_count=%d", *s.EffectiveCount))
	}
	if len(params) > 0 {
		fmt.Fprintf(w, "  Returned parameters: %s\n", strings.Join(params, ", "))
	}
}

// PrintSplitSummary writes a human-readable summary of the resolved test plan
// to w (typically os.Stderr). At parallelism > 1 it uses per-format
// TimingMetadata to break down known vs unknown cases. At parallelism == 1 the
// server skips the per-format timing fetch and emits an empty TimingMetadata,
// so there is no history breakdown. Missing metadata is not evidence of no history.
func PrintSplitSummary(w io.Writer, p TestPlan) {
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
		noun = "runnable unit"
	}

	fmt.Fprintln(w, "Split summary")
	if p.Tasks == nil {
		fmt.Fprintf(w, "  %d %s (runnable unit count unavailable)\n", nodes, pluralize(nodes, "node"))
	} else {
		fmt.Fprintf(w, "  %d %s across %d %s\n",
			total, pluralize(total, noun), nodes, pluralize(nodes, "node"))
	}
	if p.Fallback {
		fmt.Fprintln(w, "  Local non-intelligent split; timing estimates unavailable.")
		return
	}
	target, cap := "unknown", "unknown"
	if p.Settings != nil {
		if p.Settings.TargetTime != nil {
			target = fmt.Sprintf("%gs", *p.Settings.TargetTime)
		}
		if p.Settings.MaxParallelism != nil {
			cap = strconv.Itoa(*p.Settings.MaxParallelism)
		}
	}
	fmt.Fprintf(w, "  Returned constraints: target time %s; maximum nodes %s\n", target, cap)
	if p.Settings != nil && p.Settings.MaxParallelism != nil && nodes == *p.Settings.MaxParallelism && nodes > 0 {
		fmt.Fprintln(w, "  At maximum nodes (does not establish that the cap limited sizing).")
	}
	if p.TimingMetadata == nil {
		fmt.Fprintln(w, "  Timing history unavailable.")
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
// "runnable unit" when multiple formats are present. Callers pluralize as needed.
func summaryNoun(fileTotal, exampleTotal, selectorTotal int) string {
	switch {
	case fileTotal+exampleTotal+selectorTotal == 0:
		return "runnable unit"
	case exampleTotal == 0 && selectorTotal == 0:
		return "file"
	case fileTotal == 0 && selectorTotal == 0:
		return "example"
	case fileTotal == 0 && exampleTotal == 0:
		return "selector"
	default:
		return "runnable unit"
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
	itemNounFor := func(n int) string { return " " + pluralize(n, noun) }
	if mixed {
		fmt.Fprintf(w, "  %d %s\n", total, pluralize(total, noun))
		indent = "    "
		// In nested form the "files"/"examples" header carries the noun, so
		// each line just shows counts.
		itemNounFor = func(int) string { return "" }
	}

	width := len(strconv.Itoa(total))

	if known == 0 {
		suffix := " and used the default duration"
		if meta != nil {
			suffix += fmt.Sprintf(" (%s)", formatDurationMS(meta.DefaultDuration))
		}
		fmt.Fprintf(w, "%s%*d%s (100%%) had no history%s\n",
			indent, width, total, itemNounFor(total), suffix)
		return
	}

	unknown := total - known
	fmt.Fprintf(w, "%s%*d%s (%s) estimated from past historical durations\n",
		indent, width, known, itemNounFor(known), percentOf(known, total))
	if unknown > 0 {
		suffix := ""
		if meta != nil && meta.MedianDuration != nil {
			suffix = fmt.Sprintf(" — assumed median (%s)", formatDurationMS(*meta.MedianDuration))
		}
		fmt.Fprintf(w, "%s%*d%s (%s) had no history%s\n",
			indent, width, unknown, itemNounFor(unknown), percentOf(unknown, total), suffix)
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
