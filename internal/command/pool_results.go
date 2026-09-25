package command

import (
	"slices"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

// poolResults is lease-local: runner batches never contain Scheduler IDs.
type poolResults struct {
	attempts     []api.LeaseAttempt
	reported     []map[string]runner.ReportedTest
	broken       []bool
	reportErrors []bool
	muted        []plan.TestCase
}

func newPoolResults(attempts []api.LeaseAttempt, muted []plan.TestCase) *poolResults {
	r := &poolResults{attempts: attempts, muted: muted, reported: make([]map[string]runner.ReportedTest, len(attempts)), broken: make([]bool, len(attempts)), reportErrors: make([]bool, len(attempts))}
	for i := range attempts {
		r.reported[i] = map[string]runner.ReportedTest{}
	}
	return r
}

func matchesReportedTest(dispatched plan.TestCase, reported runner.ReportedTest) bool {
	target := dispatched.Path
	if dispatched.Format == plan.TestCaseFormatExample && dispatched.Identifier != "" {
		target = dispatched.Identifier
	}
	if dispatched.Format == plan.TestCaseFormatSelector {
		target = dispatched.Value
	}
	if dispatched.Format != plan.TestCaseFormatExample {
		if target != "" && target == reported.Selector {
			return true
		}
	}
	return target != "" && (target == reported.TestCase.Identifier || target == reported.TestCase.Path || target == reported.Location)
}
func (r *poolResults) isMuted(test plan.TestCase) bool {
	for _, m := range r.muted {
		if m.Name == test.Name && m.Scope == test.Scope {
			return true
		}
	}
	return false
}

// absorb maps every reported test to exactly one dispatched case. Retry owners
// retain original attempt indices. Report-level errors are sticky, but do not
// suppress retries of failed tests.
func (r *poolResults) absorb(tests []plan.TestCase, owners []int, result runnerexec.Result) {
	bad := func() {
		for _, i := range owners {
			r.broken[i] = true
		}
	}
	if result.Status != "completed" {
		bad()
		return
	}
	report, err := runner.ParseNativeReport(result.ReportFormat, result.Report)
	if err != nil {
		bad()
		return
	}
	if report.ErrorsOutsideTests {
		for _, i := range owners {
			r.reportErrors[i] = true
		}
	}
	seen := make([]bool, len(tests))
	keys := map[string]bool{}
	for _, test := range report.Tests {
		key := test.TestCase.Path
		if key == "" || keys[key] {
			bad()
			continue
		}
		keys[key] = true
		match := -1
		for i, dispatched := range tests {
			if matchesReportedTest(dispatched, test) {
				if match != -1 {
					match = -2
					break
				}
				match = i
			}
		}
		if match < 0 {
			bad()
			continue
		}
		seen[match] = true
		r.reported[owners[match]][key] = test
	}
	for i, test := range tests {
		if !seen[i] && test.Format == plan.TestCaseFormatExample {
			r.broken[owners[i]] = true
		}
	}
}

func (r *poolResults) retries() ([]plan.TestCase, []int) {
	var tests []plan.TestCase
	var owners []int
	// Sort below, rather than expose map iteration order to the runner.
	for i := range r.attempts {
		keys := make([]string, 0, len(r.reported[i]))
		for key := range r.reported[i] {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			test := r.reported[i][key]
			if test.Status == runner.TestStatusFailed && !r.isMuted(test.TestCase) {
				caseResult := test.TestCase
				caseResult.Format = plan.TestCaseFormatExample
				tests = append(tests, caseResult)
				owners = append(owners, i)
			}
		}
	}
	return tests, owners
}

func (r *poolResults) final() []api.AttemptResult {
	results := make([]api.AttemptResult, len(r.attempts))
	for i, a := range r.attempts {
		status := "passed"
		if r.reportErrors[i] {
			status = "errored"
		}
		for _, test := range r.reported[i] {
			if test.Status == runner.TestStatusFailed && !r.isMuted(test.TestCase) {
				status = "failed"
			}
		}
		if r.broken[i] {
			status = "errored"
		}
		results[i] = api.AttemptResult{AttemptID: a.ID, Result: status}
	}
	return results
}
