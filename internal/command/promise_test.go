package command

import (
	"context"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func testCase(path string) plan.TestCase {
	// Set Name as well as Path so muted-test matching (which keys on Scope+Name)
	// can target individual cases precisely.
	return plan.TestCase{Name: path, Path: path}
}

// resultWith builds a RunResult by recording the given statuses. Any test case
// passed in muted is registered as muted, so a failure on it is a muted failure.
func resultWith(muted []plan.TestCase, record func(r *runner.RunResult)) runner.RunResult {
	r := runner.NewRunResult(muted)
	record(r)
	return *r
}

func TestPromiseFailureDecision(t *testing.T) {
	hardFailures := resultWith(nil, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
		r.RecordTestResult(testCase("b_spec.rb"), runner.TestStatusFailed)
		r.RecordTestResult(testCase("c_spec.rb"), runner.TestStatusFailed)
	})

	allPassed := resultWith(nil, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
	})

	mutedOnlyFailures := resultWith([]plan.TestCase{testCase("b_spec.rb")}, func(r *runner.RunResult) {
		r.RecordTestResult(testCase("a_spec.rb"), runner.TestStatusPassed)
		r.RecordTestResult(testCase("b_spec.rb"), runner.TestStatusFailed) // muted -> not a hard failure
	})

	tests := []struct {
		name           string
		promiseEnabled bool
		result         runner.RunResult
		wantDeclare    bool
		wantReason     string
	}{
		{
			name:           "hard failures with flag on declares",
			promiseEnabled: true,
			result:         hardFailures,
			wantDeclare:    true,
			wantReason:     "test_failure (2 failed after retries)",
		},
		{
			name:           "hard failures with flag off does nothing",
			promiseEnabled: false,
			result:         hardFailures,
			wantDeclare:    false,
		},
		{
			name:           "no failures does nothing",
			promiseEnabled: true,
			result:         allPassed,
			wantDeclare:    false,
		},
		{
			name:           "muted-only failures does nothing",
			promiseEnabled: true,
			result:         mutedOnlyFailures,
			wantDeclare:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{PromiseFailure: tc.promiseEnabled}

			declare, reason := promiseFailureDecision(cfg, tc.result)

			if declare != tc.wantDeclare {
				t.Fatalf("declare = %v, want %v", declare, tc.wantDeclare)
			}
			if declare && reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestMakePromiseFailureCommand(t *testing.T) {
	cfg := &config.Config{BuildkiteAgentCommand: "buildkite-agent"}

	cmd := makePromiseFailureCommand(context.Background(), cfg, 1, "test_failure (2 failed after retries)")

	want := []string{"buildkite-agent", "job", "promise-failure", "1", "--reason", "test_failure (2 failed after retries)"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}
