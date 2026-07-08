package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func TestUploadSchedulerEntriesChunksRequests(t *testing.T) {
	var chunkSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/buildkite/test-scheduler/pools/pool/entries" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body api.CreateTestSchedulerEntriesParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		chunkSizes = append(chunkSizes, len(body.Entries))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	defer server.Close()

	client := api.NewClient(api.ClientConfig{ServerBaseURL: server.URL, AccessToken: "token", OrganizationSlug: "buildkite"})
	testCases := make([]plan.TestCase, 205)
	for i := range testCases {
		testCases[i] = plan.TestCase{Path: "spec/a_spec.rb"}
	}

	if err := uploadSchedulerEntries(context.Background(), client, "pool", testCases); err != nil {
		t.Fatalf("uploadSchedulerEntries() error = %v", err)
	}
	want := []int{100, 100, 5}
	if len(chunkSizes) != len(want) {
		t.Fatalf("chunkSizes = %v, want %v", chunkSizes, want)
	}
	for i := range want {
		if chunkSizes[i] != want[i] {
			t.Fatalf("chunkSizes = %v, want %v", chunkSizes, want)
		}
	}
}

func TestQueueAttemptsFromLeaseDecodesSelectorsAndTrimsLocationPrefix(t *testing.T) {
	lease := &api.TestSchedulerLease{ID: "lease", Attempts: []api.TestSchedulerAttempt{{
		ID:           "attempt",
		SelectorType: "custom",
		Selector: api.TestSchedulerEntrySelector{
			Format: plan.TestCaseFormatExample,
			Path:   "repo/spec/a_spec.rb[1:1]",
			Name:   "passes",
			Scope:  "A",
		},
	}}}

	attempts, testCases, err := queueAttemptsFromLease(lease, "repo/")
	if err != nil {
		t.Fatalf("queueAttemptsFromLease() error = %v", err)
	}
	if attempts[0].LeaseID != "lease" || attempts[0].AttemptID != "attempt" {
		t.Fatalf("attempt mapping = %+v", attempts[0])
	}
	if got, want := testCases[0].Path, "spec/a_spec.rb[1:1]"; got != want {
		t.Fatalf("test path = %q, want %q", got, want)
	}
}

func TestCompletionParamsForAttemptResultsUsesEachAttemptResult(t *testing.T) {
	file := plan.TestCase{Path: "spec/a_spec.rb"}
	other := plan.TestCase{Path: "spec/other_spec.rb"}

	params := completionParamsForAttemptResults([]queueAttemptResult{
		{queueAttempt: queueAttempt{LeaseID: "lease", AttemptID: "a1", TestCase: file}, SchedulerResult: "passed"},
		{queueAttempt: queueAttempt{LeaseID: "lease", AttemptID: "a2", TestCase: other}, SchedulerResult: "failed"},
	})

	got := []string{}
	for _, attempt := range params.Leases[0].Attempts {
		got = append(got, attempt.Result)
	}
	want := []string{"passed", "failed"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("completion results = %v, want %v", got, want)
		}
	}
}

func TestSchedulerResultForRun(t *testing.T) {
	tests := []struct {
		name   string
		result runner.RunResult
		want   string
	}{
		{
			name: "passed",
			result: func() runner.RunResult {
				r := runner.NewRunResult(nil)
				r.RecordTestResult(plan.TestCase{Path: "spec/a_spec.rb[1:1]"}, runner.TestStatusPassed)
				return *r
			}(),
			want: "passed",
		},
		{
			name: "failed",
			result: func() runner.RunResult {
				r := runner.NewRunResult(nil)
				r.RecordTestResult(plan.TestCase{Path: "spec/a_spec.rb[1:1]"}, runner.TestStatusFailed)
				return *r
			}(),
			want: "failed",
		},
		{
			name:   "unknown",
			result: *runner.NewRunResult(nil),
			want:   "errored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := schedulerResultForRun(tt.result); got != tt.want {
				t.Fatalf("schedulerResultForRun() = %q, want %q", got, tt.want)
			}
		})
	}
}
