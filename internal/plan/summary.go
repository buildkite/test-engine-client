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

	total := 0
	known := 0
	exampleMode := false
	for _, task := range p.Tasks {
		for _, tc := range task.Tests {
			total++
			if tc.TimingSampleSize > 0 {
				known++
			}
			if tc.Format == TestCaseFormatExample {
				exampleMode = true
			}
		}
	}
	if total == 0 {
		return
	}

	noun := "files"
	if exampleMode {
		noun = "examples"
	}

	nodes := p.Parallelism
	if nodes == 0 {
		nodes = len(p.Tasks)
	}

	fmt.Fprintln(w, "\n+++ Buildkite Test Engine Client: 📊 Split summary")
	fmt.Fprintf(w, "%d %s across %d nodes\n", total, noun, nodes)

	width := len(strconv.Itoa(total))

	if known == 0 {
		fmt.Fprintf(w, "  %*d %s (100%%) had no history and used the default duration (%s)\n\n",
			width, total, noun, formatDurationMS(p.TimingMetadata.DefaultDuration))
		return
	}

	unknown := total - known
	fmt.Fprintf(w, "  %*d %s (%d%%) estimated from past historical durations\n", width, known, noun, percentOf(known, total))
	if unknown > 0 {
		median := "unknown"
		if p.TimingMetadata.MedianDuration != nil {
			median = formatDurationMS(*p.TimingMetadata.MedianDuration)
		}
		fmt.Fprintf(w, "  %*d %s (%d%%) had no history — assumed median (%s)\n", width, unknown, noun, percentOf(unknown, total), median)
	}
	fmt.Fprintln(w)
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
