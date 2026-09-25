package command

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

func poolReport(examples string, count, loadErrors int) runnerexec.Result {
	return runnerexec.Result{Status: "completed", ReportFormat: "rspec-json", Report: json.RawMessage(fmt.Sprintf(`{"examples":%s,"summary":{"example_count":%d,"errors_outside_of_examples_count":%d}}`, examples, count, loadErrors))}
}

func TestPoolResultsRetryMappingAndMuting(t *testing.T) {
	tests := []plan.TestCase{{Format: "selector", Value: "spec/a.rb"}, {Format: "selector", Value: "spec/b.rb"}}
	r := newPoolResults([]api.LeaseAttempt{{ID: "a", Selector: tests[0]}, {ID: "b", Selector: tests[1]}}, []plan.TestCase{{Name: "muted", Scope: "B"}})
	r.absorb(tests, []int{0, 1}, poolReport(`[
 {"id":"./spec/a.rb[1:2]","file_path":"./spec/a.rb","status":"failed","description":"retry","full_description":"A retry"},
 {"id":"spec/a.rb[1:1]","file_path":"spec/a.rb","status":"passed"},
 {"id":"spec/b.rb[1:1]","file_path":"spec/b.rb","status":"failed","description":"muted","full_description":"B muted"},
 {"id":"spec/b.rb[1:2]","file_path":"spec/b.rb","status":"pending"}]`, 4, 0))
	retry, owners := r.retries()
	if len(retry) != 1 || retry[0].Format != "example" || retry[0].Identifier != "spec/a.rb[1:2]" || !reflect.DeepEqual(owners, []int{0}) {
		t.Fatalf("retry=%+v owners=%v", retry, owners)
	}
	r.absorb(retry, owners, poolReport(`[{"id":"spec/a.rb[1:2]","file_path":"spec/a.rb","status":"passed"}]`, 1, 0))
	if got := r.final(); !reflect.DeepEqual(got, []api.AttemptResult{{AttemptID: "a", Result: "passed"}, {AttemptID: "b", Result: "passed"}}) {
		t.Fatal(got)
	}
}

func TestPoolResultsSharedExampleBelongsToOwningSpec(t *testing.T) {
	tests := []plan.TestCase{
		{Format: "file", Path: "spec/specs_with_shared_examples_spec.rb"},
		{Format: "file", Path: "spec/spells/expelliarmus_spec.rb"},
	}
	r := newPoolResults([]api.LeaseAttempt{{ID: "shared", Selector: tests[0]}, {ID: "spell", Selector: tests[1]}}, nil)
	r.absorb(tests, []int{0, 1}, poolReport(`[
{"id":"./spec/specs_with_shared_examples_spec.rb[1:1:1]","file_path":"./spec/shared_examples.rb","status":"passed"},
{"id":"./spec/spells/expelliarmus_spec.rb[1:1]","file_path":"./spec/spells/expelliarmus_spec.rb","status":"passed"}]`, 2, 0))
	if got := r.final(); !reflect.DeepEqual(got, []api.AttemptResult{{AttemptID: "shared", Result: "passed"}, {AttemptID: "spell", Result: "passed"}}) {
		t.Fatalf("shared example marked both attempts errored: %v", got)
	}
	if matchesReportedTest(plan.TestCase{Format: "file", Path: "spec/specs_with_shared_examples_spec.rb"}, runner.ReportedTest{Selector: "spec/specs_with_shared_examples_spec.rb.old"}) {
		t.Fatal("matched an unrelated file prefix")
	}
}

func TestPoolResultMatchesExampleByLocationWithID(t *testing.T) {
	test := plan.TestCase{Format: "example", Path: "spec/a_spec.rb:7"}
	r := newPoolResults([]api.LeaseAttempt{{ID: "attempt", Selector: test}}, nil)
	r.absorb([]plan.TestCase{test}, []int{0}, poolReport(`[{"id":"./spec/a_spec.rb[1:2]","file_path":"./spec/a_spec.rb","line_number":7,"status":"failed"}]`, 1, 0))
	if got := r.final(); !reflect.DeepEqual(got, []api.AttemptResult{{AttemptID: "attempt", Result: "failed"}}) {
		t.Fatalf("file:line selector did not match reported example: %v", got)
	}
}

func TestPoolResultsConservativeReports(t *testing.T) {
	for _, tc := range []struct {
		name, examples string
		count, loads   int
		format         plan.TestCaseFormat
		want           string
	}{
		{"empty file", "[]", 0, 0, "file", "passed"},
		{"empty example", "[]", 0, 0, "example", "errored"},
		{"load error", "[]", 0, 1, "file", "errored"},
		{"unmapped", `[{"id":"other[1]","file_path":"other","status":"passed"}]`, 1, 0, "file", "errored"},
		{"unknown status", `[{"id":"a[1]","file_path":"a","status":"unknown"}]`, 1, 0, "file", "errored"},
		{"failure", `[{"id":"a[1]","file_path":"a","status":"failed"}]`, 1, 0, "file", "failed"},
		{"failure and load error", `[{"id":"a[1]","file_path":"a","status":"failed"}]`, 1, 1, "file", "failed"},
		{"incomplete summary", "[]", 1, 0, "file", "errored"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			test := plan.TestCase{Format: tc.format, Path: "a"}
			r := newPoolResults([]api.LeaseAttempt{{ID: "attempt", Selector: test}}, nil)
			r.absorb([]plan.TestCase{test}, []int{0}, poolReport(tc.examples, tc.count, tc.loads))
			if got := r.final()[0].Result; got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"examples":null,"summary":{}}`, `null`, `{"examples":[]} `, `{"examples":[],"summary":{}}`} {
		r := newPoolResults([]api.LeaseAttempt{{ID: "attempt"}}, nil)
		r.absorb([]plan.TestCase{{Format: "file", Path: "a"}}, []int{0}, runnerexec.Result{Status: "completed", ReportFormat: "rspec-json", Report: json.RawMessage(raw)})
		if r.final()[0].Result != "errored" {
			t.Fatalf("accepted %s", raw)
		}
	}
	r := newPoolResults([]api.LeaseAttempt{{ID: "attempt"}}, nil)
	r.absorb([]plan.TestCase{{Format: "file", Path: "a"}}, []int{0}, runnerexec.Result{Status: "completed", ReportFormat: "unrecognized", Report: json.RawMessage(`{"examples":[],"summary":{"example_count":0}}`)})
	if r.final()[0].Result != "errored" {
		t.Fatal("unsupported report format was treated as a passing empty file")
	}
}

func TestPoolLoadErrorStillRetriesFailureWithPathFallback(t *testing.T) {
	test := plan.TestCase{Format: "file", Path: "a"}
	r := newPoolResults([]api.LeaseAttempt{{ID: "original", Selector: test}}, nil)
	r.absorb([]plan.TestCase{test}, []int{0}, poolReport(`[{"file_path":"./a","line_number":12,"status":"failed"}]`, 1, 1))
	retry, owners := r.retries()
	if len(retry) != 1 || retry[0].Identifier != "" || retry[0].Path != "a:12" || retry[0].Format != "example" {
		t.Fatal(retry)
	}
	r.absorb(retry, owners, poolReport(`[{"file_path":"a","line_number":12,"status":"passed"}]`, 1, 0))
	if r.final()[0].Result != "errored" {
		t.Fatal("load error was hidden by passing retry")
	}
}
