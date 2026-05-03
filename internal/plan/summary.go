package plan

import (
	"fmt"
	"io"
	"strconv"
)

// PrintSplitSummary writes a human-readable summary of the resolved test plan
// to w (typically os.Stderr). It is a no-op when the plan does not carry
// timing metadata (fallback plans, error plans, or cached plans created
// before the server began emitting timing_metadata).
func PrintSplitSummary(w io.Writer, p TestPlan) {
	if p.Fallback || p.TimingMetadata == nil {
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
	fmt.Fprintf(w, "%d %s across %d nodes\n", total, noun, nodes)

	// At parallelism == 1 the server skips the per-format timing fetch, so
	// per-case TimingSampleSize is always 0. Fall back to the plan-level
	// known_timings_ratio for a meaningful breakdown.
	if nodes <= 1 && p.KnownTimingsRatio != nil {
		printRatioBreakdown(w, total, *p.KnownTimingsRatio, noun)
		fmt.Fprintln(w)
		return
	}

	if fileTotal > 0 {
		printFormatBreakdown(w, fileTotal, fileKnown, "files", p.TimingMetadata.File, mixed)
	}
	if exampleTotal > 0 {
		printFormatBreakdown(w, exampleTotal, exampleKnown, "examples", p.TimingMetadata.Example, mixed)
	}
	fmt.Fprintln(w)
}

// printRatioBreakdown renders a single-node summary using the plan-level
// known_timings_ratio. The known/unknown split is rounded so the two lines
// always sum to total.
func printRatioBreakdown(w io.Writer, total int, ratio float64, noun string) {
	known := int(float64(total)*ratio + 0.5)
	if known > total {
		known = total
	}
	if known < 0 {
		known = 0
	}
	unknown := total - known
	width := len(strconv.Itoa(total))

	if known > 0 {
		fmt.Fprintf(w, "  %*d %s (%d%%) estimated from past historical durations\n",
			width, known, noun, percentOf(known, total))
	}
	if unknown > 0 {
		fmt.Fprintf(w, "  %*d %s (%d%%) had no history\n",
			width, unknown, noun, percentOf(unknown, total))
	}
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

// summaryNoun picks the heading noun when the plan only contains one format,
// or "tests" when both are present.
func summaryNoun(fileTotal, exampleTotal int) string {
	switch {
	case exampleTotal == 0:
		return "files"
	case fileTotal == 0:
		return "examples"
	default:
		return "tests"
	}
}

func printFormatBreakdown(w io.Writer, total, known int, noun string, meta *FormatTimingMetadata, mixed bool) {
	indent := "  "
	itemNoun := " " + noun
	if mixed {
		fmt.Fprintf(w, "  %d %s\n", total, noun)
		indent = "    "
		// In nested form the "files"/"examples" header carries the noun, so
		// each line just shows counts.
		itemNoun = ""
	}

	width := len(strconv.Itoa(total))

	if known == 0 {
		suffix := ""
		if meta != nil {
			suffix = fmt.Sprintf(" and used the default duration (%s)", formatDurationMS(meta.DefaultDuration))
		}
		fmt.Fprintf(w, "%s%*d%s (100%%) had no history%s\n",
			indent, width, total, itemNoun, suffix)
		return
	}

	unknown := total - known
	fmt.Fprintf(w, "%s%*d%s (%d%%) estimated from past historical durations\n",
		indent, width, known, itemNoun, percentOf(known, total))
	if unknown > 0 {
		suffix := ""
		if meta != nil && meta.MedianDuration != nil {
			suffix = fmt.Sprintf(" — assumed median (%s)", formatDurationMS(*meta.MedianDuration))
		}
		fmt.Fprintf(w, "%s%*d%s (%d%%) had no history%s\n",
			indent, width, unknown, itemNoun, percentOf(unknown, total), suffix)
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
