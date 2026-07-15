package plan

import (
	"fmt"
	"io"
	"math"
	"strconv"
)

// PrintSplitSummary writes a human-readable summary of the resolved test plan
// to w (typically os.Stderr). At parallelism > 1 it uses per-format
// TimingMetadata to break down known vs unknown cases. At parallelism == 1 the
// server skips the per-format timing fetch and emits an empty TimingMetadata,
// so only the header and case count are printed. Skipped for fallback plans and
// plans without TimingMetadata at all (e.g. error or older cached plans).
func PrintSplitSummary(w io.Writer, p TestPlan) {
	if p.Fallback || p.TimingMetadata == nil {
		return
	}

	fileTotal, fileKnown := countByFormat(p, TestCaseFormatFile)
	exampleTotal, exampleKnown := countByFormat(p, TestCaseFormatExample)
	selectorTotal, selectorKnown := countByFormat(p, TestCaseFormatSelector)

	total := fileTotal + exampleTotal + selectorTotal
	if total == 0 {
		return
	}

	nodes := p.Parallelism
	mixed := countNonZero(fileTotal, exampleTotal, selectorTotal) > 1
	noun := summaryNoun(fileTotal, exampleTotal, selectorTotal)

	fmt.Fprintln(w, "\n+++ Buildkite Test Engine Client: 📊 Split summary")
	fmt.Fprintf(w, "%d %s across %d %s\n",
		total, pluralize(total, noun), nodes, pluralize(nodes, "node"))

	// At parallelism == 1 the server skips the per-format timing fetch and
	// emits an empty TimingMetadata, so there is no breakdown to print.
	if p.TimingMetadata.File == nil && p.TimingMetadata.Example == nil && p.TimingMetadata.Selector == nil {
		fmt.Fprintln(w)
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
	fmt.Fprintln(w)
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
// "test" when multiple formats are present. Callers pluralize as needed.
func summaryNoun(fileTotal, exampleTotal, selectorTotal int) string {
	switch {
	case exampleTotal == 0 && selectorTotal == 0:
		return "file"
	case fileTotal == 0 && selectorTotal == 0:
		return "example"
	case fileTotal == 0 && exampleTotal == 0:
		return "selector"
	default:
		return "test"
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
