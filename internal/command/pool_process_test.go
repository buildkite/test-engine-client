//go:build !windows

package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
	"github.com/urfave/cli/v3"
)

func TestPoolPersistentChild(t *testing.T) {
	mode := os.Getenv("BKTEC_POOL_TEST_CHILD")
	if mode == "" {
		return
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", os.Getenv(runnerexec.SocketEnv))
	}}}
	session := ""
	request := func(method, path string, body []byte) []byte {
		req, err := http.NewRequest(method, "http://runner"+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Bktec-Session", session)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			t.Fatalf("HTTP %d: %s", resp.StatusCode, raw)
		}
		if strings.Contains(string(raw), "scheduler-secret") {
			t.Fatal("Scheduler attempt ID crossed protocol")
		}
		return raw
	}
	formats := `["file","example"]`
	if mode == "unsupported" {
		formats = `["selector"]`
	}
	raw := request("POST", "/v1/sessions", []byte(`{"instance_id":"once","capabilities":{"selector_formats":`+formats+`}}`))
	var registration runnerexec.SessionResponse
	if err := json.Unmarshal(raw, &registration); err != nil {
		t.Fatal(err)
	}
	session = registration.SessionID
	batches := 0
	for {
		var response runnerexec.PullResponse
		if err := json.Unmarshal(request("POST", "/v1/batches", []byte(`{}`)), &response); err != nil {
			t.Fatal(err)
		}
		switch response.Type {
		case "wait":
			time.Sleep(time.Millisecond)
		case "done":
			if mode == "success" && batches != 3 {
				t.Fatalf("batches=%d", batches)
			}
			time.Sleep(20 * time.Millisecond)
			if err := os.WriteFile(os.Getenv("BKTEC_POOL_TEST_FLUSH"), []byte("flushed"), 0600); err != nil {
				t.Fatal(err)
			}
			return
		case "batch":
			batches++
			if mode == "crash" {
				os.Exit(19)
			}
			if mode == "delete" {
				request("DELETE", "/v1/sessions/"+session, []byte(`{"reason":"leaving"}`))
				return
			}
			if mode == "timeout" {
				time.Sleep(time.Minute)
				return
			}
			status := "passed"
			if mode != "pass" && mode != "unmapped" && (batches == 1 || mode == "failure") {
				status = "failed"
			}
			id := "a[1]"
			if mode == "unmapped" {
				id = "other[1]"
			}
			report := poolReport(fmt.Sprintf(`[{"id":%q,"file_path":"./a","status":%q}]`, id, status), 1, 0)
			raw, _ := json.Marshal(report)
			request("POST", "/v1/batches/"+response.Batch.ID+"/results", raw)
		}
	}
}

func TestPoolPersistentProcess(t *testing.T) {
	for _, mode := range []string{"success", "failure", "crash", "delete", "timeout", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s := newPoolSource(cancel)
			acquisitions, completed, released := 0, 0, 0
			f := &fakePoolScheduler{
				acquire: func(context.Context) (api.LeaseResponse, error) {
					acquisitions++
					r := api.LeaseResponse{}
					r.Pool.State = "consumed"
					if acquisitions <= 2 {
						lease := testLease()
						lease.Attempts[0].ID = "scheduler-secret"
						r.Lease = &lease
						r.Pool.State = "consuming"
					}
					return r, nil
				},
				release: func() error { released++; return nil },
				complete: func(_ context.Context, results []api.AttemptResult) error {
					completed++
					want := "passed"
					if mode == "failure" {
						want = "failed"
					}
					if mode == "crash" || mode == "delete" || mode == "timeout" {
						want = "errored"
					}
					if results[0].Result != want {
						t.Errorf("result=%v want %s", results, want)
					}
					return nil
				},
			}
			done := make(chan struct{})
			go func() { defer close(done); s.work(ctx, f, api.Pool{ID: "pool"}, 1) }()
			flush := filepath.Join(t.TempDir(), "flushed")
			cmd := exec.Command(os.Args[0], "-test.run=^TestPoolPersistentChild$")
			cmd.Env = append(os.Environ(), "BKTEC_POOL_TEST_CHILD="+mode, "BKTEC_POOL_TEST_FLUSH="+flush)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			// The race runtime adds a one-second process-exit delay.
			err := runnerexec.Run(ctx, cmd, s, runnerexec.Options{StartupTimeout: 2 * time.Second, BatchTimeout: 150 * time.Millisecond, ShutdownTimeout: 2 * time.Second})
			cancel()
			<-done
			if mode == "success" || mode == "failure" {
				if err != nil {
					t.Fatalf("runner: %v %s", err, &output)
				}
				if _, e := os.Stat(flush); e != nil {
					t.Fatal("returned before collector flush")
				}
				if completed != 2 || released != 0 {
					t.Fatal(completed, released)
				}
				if (s.outcome() != nil) != (mode == "failure") {
					t.Fatalf("verdict=%v", s.outcome())
				}
				if mode == "success" && s.passedAttempts != 2 {
					t.Fatalf("passed attempts=%d, want 2", s.passedAttempts)
				}
				if mode == "failure" && s.outcome().Error() != "pool execution: passed attempts: 0; failed attempts: 2; errored attempts: 0" {
					t.Fatalf("failed attempts not distinguished: %v", s.outcome())
				}
			} else {
				if err == nil {
					t.Fatal("missing lifecycle error")
				}
				if mode == "unsupported" {
					if released != 1 || completed != 0 {
						t.Fatal("undispatched accounting", released, completed)
					}
				} else if completed != 1 || released != 0 {
					t.Fatal("dispatched accounting", released, completed)
				}
			}
		})
	}
}

func TestPoolExecSuppliedConsumedIDSkipsRunnerAndPlanning(t *testing.T) {
	var logs bytes.Buffer
	setDebugEnabled(t, &logs)
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/org/test-scheduler/pools/existing" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		gets++
		_, _ = io.WriteString(w, `{"id":"existing","state":"consumed","muted_tests":[]}`)
	}))
	defer server.Close()

	// Only pool-id is set: any discovery or planning request would fail.
	cfg := config.New()
	cfg.PoolID = "existing"
	cfg.OrganizationSlug = "org"
	cfg.SuiteSlug = "suite"
	cfg.ServerBaseURL = server.URL
	cfg.OIDC = true
	cfg.OIDCLifetime = time.Hour
	cfg.BuildkiteAgentCommand = filepath.Join("..", "config", "mock-buildkite-agent")
	err := PoolExec(context.Background(), &cfg, "", []string{filepath.Join(t.TempDir(), "nonexistent-runner")}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if gets != 1 {
		t.Fatalf("GET count=%d, want one full snapshot for ready pool ID", gets)
	}
	if !strings.Contains(logs.String(), "Pool already consumed; skipping persistent runner") || strings.Contains(logs.String(), "starting persistent runner") {
		t.Fatalf("wrong consumed-pool log: %s", logs.String())
	}
}

func TestPoolExecSuppliedConsumingIDReusesSnapshot(t *testing.T) {
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/org/test-scheduler/pools/existing":
			gets++
			_, _ = io.WriteString(w, `{"id":"existing","state":"consuming","muted_tests":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/org/test-scheduler/pools/existing/leases":
			_, _ = io.WriteString(w, `{"lease":null,"pool":{"id":"existing","state":"consumed"}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	flush := filepath.Join(t.TempDir(), "flushed")
	t.Setenv("BKTEC_POOL_TEST_CHILD", "empty")
	t.Setenv("BKTEC_POOL_TEST_FLUSH", flush)
	cfg := config.New()
	cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug = "existing", "org", "suite"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	if err := PoolExec(context.Background(), &cfg, "", []string{os.Args[0], "-test.run=^TestPoolPersistentChild$"}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if gets != 1 {
		t.Fatalf("GET count=%d, want one full snapshot for consuming pool ID", gets)
	}
}

func TestPoolExecIdempotentPlanAlreadyConsumingSkipsReadinessGet(t *testing.T) {
	var logs bytes.Buffer
	setDebugEnabled(t, &logs)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/filter_tests"):
			_, _ = io.WriteString(w, `{"tests":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/org/test-scheduler/pools/plan":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"existing","state":"consuming","muted_tests":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/org/test-scheduler/pools/existing/leases":
			_, _ = io.WriteString(w, `{"lease":null,"pool":{"id":"existing","state":"consumed"}}`)
		default:
			t.Errorf("unexpected request after ready planning response: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	files := filepath.Join(t.TempDir(), "files")
	if err := os.WriteFile(files, []byte("spec/a_spec.rb\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flush := filepath.Join(t.TempDir(), "flushed")
	t.Setenv("BKTEC_POOL_TEST_CHILD", "empty")
	t.Setenv("BKTEC_POOL_TEST_FLUSH", flush)
	cfg := config.New()
	cfg.OrganizationSlug, cfg.SuiteSlug, cfg.TestRunner = "org", "suite", "rspec"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	cfg.PoolKey, cfg.BuildID, cfg.PipelineSlug = "key", "build", "pipeline"
	if err := PoolExec(context.Background(), &cfg, files, []string{os.Args[0], "-test.run=^TestPoolPersistentChild$"}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	filtered := strings.Index(logs.String(), "No filtered selector-backed files found")
	start := strings.Index(logs.String(), "Creating or reusing pool with key key")
	resolved := strings.Index(logs.String(), "Pool existing resolved (state=consuming)")
	if filtered < 0 || start <= filtered || resolved <= start {
		t.Fatalf("expected discovery and filtering before resolution: %s", logs.String())
	}
}

func TestPoolExecStartsLeasingWhilePopulating(t *testing.T) {
	var logs bytes.Buffer
	setDebugEnabled(t, &logs)
	gets, acquisitions, completions := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/v2/organizations/org/test-scheduler/pools/existing"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			gets++
			_, _ = io.WriteString(w, `{"id":"existing","state":"populating"}`)
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
			acquisitions++
			switch acquisitions {
			case 1:
				_, _ = io.WriteString(w, `{"lease":null,"pool":{"state":"populating"}}`)
			case 2:
				fmt.Fprintf(w, `{"lease":{"id":"lease","expires_at":%q,"attempts":[{"id":"attempt","selector_type":"test_plan_test_case_v1","selector":{"format":"file","path":"a"}}]},"pool":{"state":"populating"}}`, time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
			default:
				_, _ = io.WriteString(w, `{"lease":null,"pool":{"state":"consumed"}}`)
			}
		case r.Method == http.MethodPost && r.URL.Path == base+"/leases/complete":
			var body struct {
				Leases []struct {
					Attempts []api.AttemptResult `json:"attempts"`
				} `json:"leases"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Leases) != 1 || len(body.Leases[0].Attempts) != 1 || body.Leases[0].Attempts[0].Result != "passed" {
				t.Errorf("unexpected completion: %+v, %v", body, err)
				http.Error(w, "unexpected completion", http.StatusBadRequest)
				return
			}
			completions++
			_, _ = io.WriteString(w, `{"leases":[{"lease_id":"lease","attempts":[{"id":"attempt","result":"passed","completion_status":"completed"}]}]}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()
	flush := filepath.Join(t.TempDir(), "flushed")
	t.Setenv("BKTEC_POOL_TEST_CHILD", "pass")
	t.Setenv("BKTEC_POOL_TEST_FLUSH", flush)
	cfg := config.New()
	cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug = "existing", "org", "suite"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	if err := PoolExec(context.Background(), &cfg, "", []string{os.Args[0], "-test.run=^TestPoolPersistentChild$"}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if gets != 2 || acquisitions != 3 || completions != 1 {
		t.Fatalf("gets=%d acquisitions=%d completions=%d", gets, acquisitions, completions)
	}
	if strings.Count(logs.String(), "existing") != 1 || !strings.Contains(logs.String(), "Fetching supplied pool with ID existing") || !strings.Contains(logs.String(), "Pool resolved (state=populating)") || !strings.Contains(logs.String(), "Received batch b_1 result") {
		t.Fatalf("supplied pool ID should appear only at fetch: %s", logs.String())
	}
}

func TestPoolExecSuppliedPlanningIDBecomesConsumedWithoutRunner(t *testing.T) {
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/org/test-scheduler/pools/existing" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		gets++
		if gets <= 2 {
			_, _ = io.WriteString(w, `{"id":"existing","state":"planning"}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"existing","state":"consumed","muted_tests":[]}`)
		}
	}))
	defer server.Close()
	cfg := config.New()
	cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug = "existing", "org", "suite"
	cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
	if err := PoolExec(context.Background(), &cfg, "", []string{filepath.Join(t.TempDir(), "nonexistent-runner")}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if gets != 3 {
		t.Fatalf("GET count=%d, want resolution and readiness polling", gets)
	}
}

func TestPoolExecSuppliedJWTIsInitialAuth(t *testing.T) {
	for _, oidc := range []bool{false, true} {
		t.Run(fmt.Sprintf("agent-enabled=%t", oidc), func(t *testing.T) {
			token := testPoolJWT(time.Now().Add(5 * time.Minute))
			gets := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gets++
				if r.Header.Get("Authorization") != "Bearer "+token || r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/org/test-scheduler/pools/existing" {
					t.Errorf("unexpected authenticated request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(w, `{"id":"existing","state":"consumed","muted_tests":[]}`)
			}))
			defer server.Close()
			cfg := config.New()
			cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug = "existing", "org", "suite"
			cfg.ServerBaseURL, cfg.AccessToken = server.URL, token
			cfg.OIDC = oidc
			cfg.BuildkiteAgentCommand = filepath.Join(t.TempDir(), "no-agent")
			if err := PoolExec(context.Background(), &cfg, "", []string{os.Args[0], "-test.run=^TestPoolPersistentChild$"}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}); err != nil {
				t.Fatal(err)
			}
			if gets != 1 {
				t.Fatalf("GET count=%d, want one full snapshot for ready pool ID", gets)
			}
		})
	}
}

func TestPoolExecExitStatusDistinguishesFailedAttemptsAndRunnerErrors(t *testing.T) {
	for _, tc := range []struct {
		mode, result string
		wantCode     int
	}{
		{mode: "failure", result: "failed", wantCode: 1},
		{mode: "unmapped", result: "errored", wantCode: 16},
		{mode: "crash", result: "errored", wantCode: 19},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			acquisitions, completions := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				base := "/v2/organizations/org/test-scheduler/pools/existing"
				switch {
				case r.Method == http.MethodGet && r.URL.Path == base:
					fmt.Fprint(w, `{"id":"existing","state":"consuming","muted_tests":[]}`)
				case r.Method == http.MethodPost && r.URL.Path == base+"/leases":
					acquisitions++
					if acquisitions == 1 {
						fmt.Fprintf(w, `{"lease":{"id":"lease","expires_at":%q,"attempts":[{"id":"attempt","selector_type":"test_plan_test_case_v1","selector":{"format":"file","path":"a"}}]},"pool":{"state":"consuming"}}`, time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
					} else {
						fmt.Fprint(w, `{"lease":null,"pool":{"state":"consumed"}}`)
					}
				case r.Method == http.MethodPost && r.URL.Path == base+"/leases/complete":
					var body struct {
						Leases []struct {
							Attempts []api.AttemptResult `json:"attempts"`
						} `json:"leases"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Leases) != 1 || len(body.Leases[0].Attempts) != 1 || body.Leases[0].Attempts[0].Result != tc.result {
						t.Errorf("completion=%+v err=%v", body, err)
					}
					completions++
					fmt.Fprintf(w, `{"leases":[{"lease_id":"lease","attempts":[{"id":"attempt","result":%q,"completion_status":"completed"}]}]}`, tc.result)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusNotFound)
				}
			}))
			defer server.Close()
			t.Setenv("BKTEC_POOL_TEST_CHILD", tc.mode)
			t.Setenv("BKTEC_POOL_TEST_FLUSH", filepath.Join(t.TempDir(), "flushed"))
			cfg := config.New()
			cfg.PoolID, cfg.OrganizationSlug, cfg.SuiteSlug = "existing", "org", "suite"
			cfg.ServerBaseURL, cfg.AccessToken, cfg.OIDC = server.URL, "header.payload.signature", false
			err := PoolExec(context.Background(), &cfg, "", []string{os.Args[0], "-test.run=^TestPoolPersistentChild$"}, 0, runnerexec.Options{StartupTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second})
			if completions != 1 {
				t.Fatalf("completions=%d err=%v", completions, err)
			}
			var coded cli.ExitCoder
			var child *exec.ExitError
			switch tc.mode {
			case "failure":
				if !errors.As(err, &coded) || coded.ExitCode() != tc.wantCode {
					t.Fatalf("failed attempts should exit 1: %v", err)
				}
			case "unmapped":
				if err == nil || errors.As(err, &coded) || errors.As(err, &child) || !strings.Contains(err.Error(), "errored attempts: 1") {
					t.Fatalf("unresolved report should remain an internal error (16): %v", err)
				}
			case "crash":
				if !errors.As(err, &child) || child.ExitCode() != tc.wantCode {
					t.Fatalf("runner exit status should be preserved: %v", err)
				}
			}
		})
	}
}
