package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/debug"
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
		return errors.New("pool exec requires a supplied suite-audience OIDC token or agent OIDC authentication")
	}
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
		return err
	}
	if cfg.PoolID != "" {
		debug.Printf("Pool resolved (state=%s)", pool.State)
	} else {
		debug.Printf("Pool %s resolved (state=%s)", pool.ID, pool.State)
	}
	switch pool.State {
	case "planning":
		pool, err = client.WaitForPool(ctx, pool.ID)
		if err != nil {
			return err
		}
	case "populating", "consuming", "consumed":
		// GET and idempotent planning POST return the same representation.
	default:
		return fmt.Errorf("test pool %s has unsupported state %q", pool.ID, pool.State)
	}
	if pool.State == "consumed" {
		debug.Printf("Pool already consumed; skipping persistent runner")
		return nil
	}
	debug.Printf("Pool ready (state=%s); starting persistent runner", pool.State)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source := newPoolSource(cancel)
	done := make(chan struct{})
	go func() { defer close(done); source.work(ctx, client, pool, retries) }()
	// #nosec G204 G702 -- the runner executable and arguments are explicitly supplied by the CLI user after --.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = runnerexec.Run(ctx, cmd, source, opts)
	cancel()
	<-done
	outcome := source.outcome()
	debug.Printf("Pool runner stopped (runner error=%v, worker error=%v)", err, outcome)
	if err == nil && outcome == nil {
		fmt.Fprintf(os.Stdout, "pool execution: passed attempts: %d; failed attempts: 0; errored attempts: 0\n", source.passedAttempts)
	}
	if err == nil && source.err == nil && source.erroredAttempts == 0 && source.failedAttempts > 0 {
		return cli.Exit(outcome, 1)
	}
	return errors.Join(err, outcome)
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
