package plan

import (
	"fmt"
	"io"
	"strconv"
)

// PrintSplitSummary writes a human-readable summary of the resolved test plan
// to w (typically os.Stderr). At parallelism > 1 it uses per-format
// TimingMetadata to break down known vs unknown cases. At parallelism == 1
// the server skips per-format timing data due to performance reasons, so it falls back to the
// plan-level KnownTimingsRatio. Skipped for fallback plans, or when neither
// signal is available.
func PrintSplitSummary(w io.Writer, p TestPlan) {
	if p.Fallback || (p.TimingMetadata == nil && p.KnownTimingsRatio == nil) {
		return
	}

	fileTotal, fileKnown := countByFormat(p, TestCaseFormatFile)
	exampleTotal, exampleKnown := countByFormat(p, TestCaseFormatExample)

	total := fileTotal + exampleTotal
	if total == 0 {
		return
	}

	nodes := p.Parallelism
	if nodes == 0 {
		nodes = len(p.Tasks)
	}

	mixed := fileTotal > 0 && exampleTotal > 0
	noun := summaryNoun(fileTotal, exampleTotal)

	fmt.Fprintln(w, "\n+++ Buildkite Test Engine Client: 📊 Split summary")
	fmt.Fprintf(w, "%d %s across %d %s\n",
		total, pluralize(total, noun), nodes, pluralize(nodes, "node"))

	// At parallelism == 1 the server skips the per-format timing fetch, so
	// per-case TimingSampleSize is always 0. Fall back to the plan-level
	// known_timings_ratio for a meaningful breakdown.
	if nodes <= 1 && p.KnownTimingsRatio != nil {
		printRatioBreakdown(w, total, *p.KnownTimingsRatio, noun)
		fmt.Fprintln(w)
		return
	}

	if p.TimingMetadata == nil {
		fmt.Fprintln(w)
		return
	}

	if fileTotal > 0 {
		printFormatBreakdown(w, fileTotal, fileKnown, "file", p.TimingMetadata.File, mixed)
	}
	if exampleTotal > 0 {
		printFormatBreakdown(w, exampleTotal, exampleKnown, "example", p.TimingMetadata.Example, mixed)
	}
	fmt.Fprintln(w)
}

// printRatioBreakdown renders a single-node summary using the plan-level
// known_timings_ratio. Per-format metadata is unavailable at parallelism == 1,
// so it delegates to printFormatBreakdown with meta == nil; the breakdown text
// is rendered without parenthesised duration values.
func printRatioBreakdown(w io.Writer, total int, ratio float64, noun string) {
	known := int(float64(total)*ratio + 0.5)
	if known > total {
		known = total
	}
	if known < 0 {
		known = 0
	}
	printFormatBreakdown(w, total, known, noun, nil, false)
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

// summaryNoun returns the singular heading noun. The plan-level summary uses
// "file"/"example" when the plan only contains one format, or "test" when
// both are present. Callers pluralize as needed.
func summaryNoun(fileTotal, exampleTotal int) string {
	switch {
	case exampleTotal == 0:
		return "file"
	case fileTotal == 0:
		return "example"
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
	fmt.Fprintf(w, "%s%*d%s (%d%%) estimated from past historical durations\n",
		indent, width, known, itemNounFor(known), percentOf(known, total))
	if unknown > 0 {
		suffix := ""
		if meta != nil && meta.MedianDuration != nil {
			suffix = fmt.Sprintf(" — assumed median (%s)", formatDurationMS(*meta.MedianDuration))
		}
		fmt.Fprintf(w, "%s%*d%s (%d%%) had no history%s\n",
			indent, width, unknown, itemNounFor(unknown), percentOf(unknown, total), suffix)
	}
}

func percentOf(n, total int) int {
	if total == 0 {
		return 0
	}
	return n * 100 / total
}

func formatDurationMS(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", int(ms+0.5))
	}
	return fmt.Sprintf("%.1fs", ms/1000.0)
}
