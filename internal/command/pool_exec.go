package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
	"github.com/buildkite/test-engine-client/v3/internal/plan"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
	"github.com/urfave/cli/v3"
)

// PoolExec resolves directly (no separate pool plan invocation is required), then
// runs one persistent process. Native bktec uploads are never invoked here.
func PoolExec(ctx context.Context, cfg *config.Config, files string, argv []string, retries int, opts runnerexec.Options) error {
	if len(argv) == 0 || retries < 0 {
		return errors.New("pool exec requires a runner executable and a nonnegative local retry count")
	}
	if cfg.AccessToken == "" && !cfg.OIDC {
		return errors.New("pool exec requires a supplied Scheduler OIDC token or agent OIDC authentication")
	}
	printPlanningBanner(os.Stderr)
	var mint func(context.Context) (string, error)
	if cfg.OIDC {
		mint = cfg.RequestSchedulerOIDCToken
	}
	client := api.NewClient(api.ClientConfig{OrganizationSlug: cfg.OrganizationSlug, ServerBaseURL: cfg.ServerBaseURL, TokenProvider: refreshingPoolToken(cfg.AccessToken, mint, cfg.OIDCLifetime)})
	if cfg.PoolID != "" {
		debug.Printf("Fetching supplied pool with ID %s", cfg.PoolID)
	}
	pool, err := ResolvePool(ctx, cfg, files, client)
	if err != nil {
		return poolExecFallback(ctx, cfg, err, files, argv, retries, opts)
	}
	if cfg.PoolID != "" {
		debug.Printf("Pool resolved (state=%s)", pool.State)
	} else {
		debug.Printf("Pool %s resolved (state=%s)", pool.ID, pool.State)
	}
	if cfg.PoolID == "" {
		printPlanningPool(os.Stderr, cfg, pool)
	} else {
		// Like run's "Using existing plan": the pool was planned elsewhere.
		fmt.Fprintf(os.Stderr, "\nUsing existing pool\n  ID: %s\n", boundedRequestValue(pool.ID))
	}
	switch pool.State {
	case "planning":
		fmt.Fprintln(os.Stderr, "  Waiting for planning to finish...")
		pool, err = client.WaitForPool(ctx, pool.ID)
		if err != nil {
			return poolExecFallback(ctx, cfg, err, files, argv, retries, opts)
		}
	case "populating", "consuming", "consumed":
		// GET and idempotent planning POST return the same representation.
	default:
		return fmt.Errorf("test pool %s has unsupported state %q", pool.ID, pool.State)
	}
	if cfg.PoolID == "" && cfg.MaxParallelism > 0 && pool.Parallelism != nil {
		fmt.Fprintf(os.Stderr, "  Parallelism: %d (recommended)\n", *pool.Parallelism)
	}
	if pool.State == "consumed" {
		// A job retry can't re-run a consumed pool's tests, so passing would hide
		// the failures that made the job red. A pool without failures is fine: its
		// entries finished on other workers, e.g. after a spot termination.
		if cfg.JobRetryCount > 0 {
			results, err := client.WaitForPoolResults(ctx, pool.ID)
			if err != nil {
				return err
			}
			if results.Failed > 0 || results.Errored > 0 {
				return cli.Exit(fmt.Sprintf("This Test Scheduler pool has already finished with %d failed and %d errored attempts. Retrying a job doesn't re-run its tests; rebuild instead, or use --local-retry-count to retry failed tests within a worker.", results.Failed, results.Errored), 1)
			}
		}
		fmt.Println("+++ Buildkite Test Engine Client: No tests to run on this node")
		debug.Printf("Pool already consumed; skipping persistent runner")
		return nil
	}
	// Server-planned pools publish entries, the muted-tests snapshot, and their
	// ready state atomically. An externally visible ready pool without a snapshot
	// is therefore a manual pool, which pool exec cannot account for safely.
	if pool.MutedTests == nil {
		return fmt.Errorf("test pool %s is missing its muted tests snapshot", pool.ID)
	}
	debug.Printf("Pool ready (state=%s); starting persistent runner", pool.State)
	return runPool(ctx, client, pool, argv, retries, opts, leaseLog)
}

// poolExecFallback runs this job's share of a static split when Test Scheduler
// is unavailable before leasing starts, like bktec run's local fallback. Once
// leasing has started, other workers may already have run some tests, so
// failures after that point are never recovered this way.
func poolExecFallback(ctx context.Context, cfg *config.Config, err error, files string, argv []string, retries int, opts runnerexec.Options) error {
	if !poolSchedulerUnavailable(ctx, err) {
		return err
	}
	warnPoolFallback(err)
	if cfg.Parallelism < 1 || cfg.NodeIndex < 0 || cfg.NodeIndex >= cfg.Parallelism {
		return fmt.Errorf("%w; cannot fall back to a static split: BUILDKITE_PARALLEL_JOB (%d) must be less than BUILDKITE_PARALLEL_JOB_COUNT (%d)", err, cfg.NodeIndex, cfg.Parallelism)
	}
	testRunner, detectErr := runner.DetectRunner(cfg)
	if detectErr != nil {
		return fmt.Errorf("%w; cannot fall back to a static split: %w", err, detectErr)
	}
	targets, discoverErr := getTestTargets(cfg, testRunner, files)
	if discoverErr != nil {
		return fmt.Errorf("%w; cannot fall back to a static split: %w", err, discoverErr)
	}
	fallback := plan.CreateFallbackPlan(targets, cfg.Parallelism)
	printPlanningSummary(os.Stderr, fallback, "local fallback", cfg)
	share := fallback.Tasks[strconv.Itoa(cfg.NodeIndex)].Tests
	if len(share) == 0 {
		fmt.Println("+++ Buildkite Test Engine Client: No tests to run on this node")
		return nil
	}
	scheduler := &fallbackScheduler{}
	for i, test := range share {
		// One selector per lease keeps each batch well inside the runner batch
		// timeout, since a local split has no timings to size batches with.
		scheduler.leases = append(scheduler.leases, api.Lease{
			ID: fmt.Sprintf("fallback-%d", i+1),
			Attempts: []api.LeaseAttempt{{
				ID:           fmt.Sprintf("fallback-%d", i+1),
				SelectorType: "test_plan_test_case_v1",
				Selector:     plan.TestCase{Format: plan.TestCaseFormatSelector, Value: test.Path},
			}},
		})
	}
	// Local leases are an implementation detail; like bktec run, show only the
	// runner's output and the final result.
	return runPool(ctx, scheduler, api.Pool{ID: "fallback", State: "consuming"}, argv, retries, opts, log.New(io.Discard, "", 0))
}

// fallbackLeaseTTL is how long a local fallback lease stays owned between
// heartbeats; nothing else can take its tests.
const fallbackLeaseTTL = 10 * time.Minute

// fallbackScheduler serves a static split's share as local leases, so the
// fallback reuses pool exec's runner protocol, local retries and results.
type fallbackScheduler struct {
	mu     sync.Mutex
	leases []api.Lease
}

func (f *fallbackScheduler) AcquireLease(context.Context, string) (api.LeaseResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var response api.LeaseResponse
	response.Pool.State = "consumed"
	if len(f.leases) > 0 {
		lease := f.leases[0]
		f.leases = f.leases[1:]
		lease.ExpiresAt = time.Now().Add(fallbackLeaseTTL)
		response.Lease = &lease
		response.Pool.State = "consuming"
	}
	return response, nil
}

func (f *fallbackScheduler) HeartbeatLease(context.Context, string, string) (time.Time, error) {
	return time.Now().Add(fallbackLeaseTTL), nil
}

func (f *fallbackScheduler) CompleteLease(context.Context, string, string, []api.AttemptResult) error {
	return nil
}

func (f *fallbackScheduler) ReleaseLease(context.Context, string, string) error { return nil }

func runPool(ctx context.Context, client poolScheduler, pool api.Pool, argv []string, retries int, opts runnerexec.Options, leases *log.Logger) error {
	fmt.Println("+++ Buildkite Test Engine Client: Running tests")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source := newPoolSource(cancel)
	source.leaseLog = leases
	done := make(chan struct{})
	go func() { defer close(done); source.work(ctx, client, pool, retries) }()
	// #nosec G204 G702 -- the runner executable and arguments are explicitly supplied by the CLI user after --.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := runnerexec.Run(ctx, cmd, source, opts)
	cancel()
	<-done
	outcome := source.outcome()
	printPoolSummary(os.Stdout, source)
	debug.Printf("Pool runner stopped (runner error=%v, worker error=%v)", err, outcome)
	if err == nil && outcome == nil {
		fmt.Fprintf(os.Stdout, "pool execution: passed attempts: %d; failed attempts: 0; errored attempts: 0\n", source.passedAttempts)
	}
	if err == nil && source.err == nil && source.erroredAttempts == 0 && source.failedAttempts > 0 {
		return cli.Exit(outcome, 1)
	}
	return errors.Join(err, outcome)
}

func printPoolSummary(w io.Writer, source *poolSource) {
	failed, errored := source.summary()
	if len(failed) == 0 && len(errored) == 0 {
		return
	}
	fmt.Fprintln(w, "+++ ========== Buildkite Test Engine Pool Summary ==========")
	if len(failed) > 0 {
		fmt.Fprintln(w, "❌ Failed Entries:")
		entry := ""
		examplesStarted := false
		for _, test := range failed {
			if test.entry.Format == plan.TestCaseFormatExample {
				if !examplesStarted {
					fmt.Fprintln(w)
					examplesStarted = true
				}
				fmt.Fprintf(w, "- %s\n", reportedTestDescription(test.test))
				continue
			}
			if next := attemptSelector(test.entry); next != entry {
				if entry != "" {
					fmt.Fprintln(w)
				}
				entry = next
				fmt.Fprintf(w, "- %s:\n", entry)
			}
			fmt.Fprintf(w, "  - %s\n", reportedTestDescription(test.test))
		}
	}
	if len(errored) > 0 {
		if len(failed) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "🚨 Errored Attempts:")
		for _, attempt := range errored {
			selector := attemptSelector(attempt.Selector)
			if selector == "" {
				fmt.Fprintf(w, "- attempt %s\n", attempt.ID)
			} else {
				fmt.Fprintf(w, "- %s (attempt %s)\n", selector, attempt.ID)
			}
		}
	}
	fmt.Fprintln(w, "==========================================================")
}

func reportedTestDescription(test runner.ReportedTest) string {
	name := strings.TrimSpace(test.TestCase.Scope + " " + test.TestCase.Name)
	reference := reportedTestReference(test)
	if name == "" {
		return reference
	}
	if reference == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, reference)
}

func reportedTestReference(test runner.ReportedTest) string {
	if test.Location != "" {
		locationFile := test.Location
		if colon := strings.LastIndexByte(locationFile, ':'); colon >= 0 {
			locationFile = locationFile[:colon]
		}
		if strings.TrimPrefix(locationFile, "./") == strings.TrimPrefix(test.Selector, "./") {
			return test.Location
		}
	}
	if test.TestCase.Identifier != "" {
		return test.TestCase.Identifier
	}
	if test.TestCase.Path != "" {
		return test.TestCase.Path
	}
	return test.Location
}

func attemptSelector(test plan.TestCase) string {
	switch test.Format {
	case plan.TestCaseFormatSelector:
		return test.Value
	case plan.TestCaseFormatExample:
		if test.Identifier != "" {
			return test.Identifier
		}
	}
	return test.Path
}

// jwtExpiration is only for scheduling refresh, not for authenticating a token;
// the Scheduler verifies the supplied token's signature and claims.
func jwtExpiration(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(data, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

func refreshingPoolToken(initial string, request func(context.Context) (string, error), lifetime time.Duration) func(context.Context) (string, error) {
	var mu sync.Mutex
	token := initial
	expires := jwtExpiration(initial)
	refresh := time.Time{}
	if !expires.IsZero() && request != nil {
		refresh = time.Now().Add(time.Until(expires) / 2)
	} else if request != nil && initial != "" {
		// Unknown supplied expiry: use it once, then mint rather than guessing.
		refresh = time.Now()
	}
	first := initial != ""
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if first {
			first = false
			if expires.IsZero() || time.Now().Before(expires) {
				return token, nil
			}
		}
		if token != "" && (refresh.IsZero() || time.Now().Before(refresh)) && (expires.IsZero() || time.Now().Before(expires)) {
			return token, nil
		}
		if request == nil {
			return "", errors.New("supplied Scheduler OIDC token expired; enable OIDC to refresh it")
		}
		next, err := request(ctx)
		if err != nil {
			if token == initial && initial != "" {
				if expires.IsZero() || time.Now().Before(expires) {
					debug.Printf("Pool token refresh unavailable; using supplied token until expiry")
					return token, nil
				}
				return "", fmt.Errorf("supplied Scheduler OIDC token expired and agent refresh failed: %w", err)
			}
			return "", err
		}
		if next == "" {
			return "", errors.New("agent returned an empty Scheduler OIDC token")
		}
		token = next
		// Refresh halfway through the requested lifetime; never serve a stale token
		// when refresh fails. A zero lifetime disables caching, not authentication.
		refresh = time.Now().Add(lifetime / 2)
		expires = jwtExpiration(next)
		if !expires.IsZero() && !time.Now().Before(expires) {
			return "", errors.New("agent returned an expired Scheduler OIDC token")
		}
		if !expires.IsZero() && refresh.After(time.Now().Add(time.Until(expires)/2)) {
			refresh = time.Now().Add(time.Until(expires) / 2)
		}
		return token, nil
	}
}
