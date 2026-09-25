package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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

func TestPoolTokenRefresh(t *testing.T) {
	calls := 0
	provider := refreshingPoolToken("", func(context.Context) (string, error) {
		calls++
		if calls == 3 {
			return "", errors.New("mint failed")
		}
		return []string{"first", "second"}[calls-1], nil
	}, 20*time.Millisecond)
	ctx := context.Background()
	first, _ := provider(ctx)
	cached, _ := provider(ctx)
	if first != "first" || cached != "first" || calls != 1 {
		t.Fatal("cache not used")
	}
	time.Sleep(15 * time.Millisecond)
	next, _ := provider(ctx)
	if next != "second" {
		t.Fatal(next)
	}
	time.Sleep(15 * time.Millisecond)
	if token, err := provider(ctx); token != "" || err == nil {
		t.Fatal("served stale token")
	}
}

func testPoolJWT(expires time.Time) string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expires.Unix()))) + ".signature"
}

func TestSuppliedPoolTokenInitialAndRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		initial := testPoolJWT(time.Now().Add(120 * time.Second))
		calls := 0
		provider := refreshingPoolToken(initial, func(context.Context) (string, error) {
			calls++
			return "new-token", nil
		}, time.Hour)
		ctx := context.Background()
		if got, _ := provider(ctx); got != initial {
			t.Fatal("supplied JWT was not used first")
		}
		if got, _ := provider(ctx); got != initial || calls != 0 {
			t.Fatal("renewed initial JWT too soon")
		}
		time.Sleep(70 * time.Second)
		if got, err := provider(ctx); err != nil || got != "new-token" || calls != 1 {
			t.Fatalf("renewal got %q, calls=%d, err=%v", got, calls, err)
		}
	})
}

func TestSuppliedPoolTokenUnknownExpiryRefreshesAfterInitialRequest(t *testing.T) {
	calls := 0
	provider := refreshingPoolToken("header.payload.signature", func(context.Context) (string, error) {
		calls++
		return "new-token", nil
	}, time.Hour)
	if got, err := provider(context.Background()); got != "header.payload.signature" || err != nil || calls != 0 {
		t.Fatalf("first token=%q, mint calls=%d, err=%v", got, calls, err)
	}
	if got, err := provider(context.Background()); got != "new-token" || err != nil || calls != 1 {
		t.Fatalf("second token=%q, mint calls=%d, err=%v", got, calls, err)
	}
}

func TestSuppliedPoolTokenFilterTestsWhenAgentUnavailable(t *testing.T) {
	const token = "header.payload.signature"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v2/analytics/organizations/org/suites/suite/test_plan/filter_tests" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected authenticated request %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"tests":[]}`)
	}))
	defer server.Close()
	mints := 0
	client := api.NewClient(api.ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "org", TokenProvider: refreshingPoolToken(token, func(context.Context) (string, error) {
		mints++
		return "", errors.New("Missing agent-access-token")
	}, time.Hour)})
	for range 2 {
		if _, err := client.FilterTests(context.Background(), "suite", api.FilterTestsParams{}); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 || mints != 1 {
		t.Fatalf("filter requests=%d agent mints=%d, want 2 and 1", requests, mints)
	}
}

func TestSuppliedPoolTokenExpiredAndAgentUnavailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		initial := testPoolJWT(time.Now().Add(10 * time.Second))
		provider := refreshingPoolToken(initial, func(context.Context) (string, error) {
			return "", errors.New("Missing agent-access-token")
		}, time.Hour)
		if got, err := provider(context.Background()); got != initial || err != nil {
			t.Fatalf("initial token=%q err=%v", got, err)
		}
		time.Sleep(6 * time.Second)
		if got, err := provider(context.Background()); got != initial || err != nil {
			t.Fatalf("valid token after failed refresh=%q err=%v", got, err)
		}
		time.Sleep(5 * time.Second)
		if got, err := provider(context.Background()); got != "" || err == nil || !strings.Contains(err.Error(), "expired and agent refresh failed") {
			t.Fatalf("expired token=%q err=%v", got, err)
		}
	})
}

func TestSuppliedPoolTokenWithoutOIDCExpiresClearly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		initial := testPoolJWT(time.Now().Add(10 * time.Second))
		provider := refreshingPoolToken(initial, nil, 0)
		if got, err := provider(context.Background()); err != nil || got != initial {
			t.Fatal("supplied JWT not used without agent")
		}
		time.Sleep(6 * time.Second)
		if got, err := provider(context.Background()); err != nil || got != initial {
			t.Fatalf("token before expiry: %q, %v", got, err)
		}
		time.Sleep(5 * time.Second)
		if got, err := provider(context.Background()); got != "" || err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("expired token: %q, %v", got, err)
		}
	})
}
