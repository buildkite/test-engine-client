package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/command"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestRunPlanOut(t *testing.T) {
	serverPlan := `{
		"identifier":"my-plan","parallelism":2,
		"tasks":{
			"0":{"node_number":0,"tests":[{"format":"selector","path":"app/apple","identifier":"apple-id","value":"apple"}]},
			"1":{"node_number":1,"tests":[{"format":"file","path":"app/banana"}]}
		},
		"selection":{"strategy":"percent","params":{"percent":50},"applied":true,"skipped_reason":null},
		"skipped_tests":[{"format":"example","path":"app/cherry[1:1]","skipped_reason":"selection"}],
		"server_only":{"future_field":true,"decimal":1.0,"exponent":1e3,"negative_zero":-0.0,"large_integer":12345678901234567890123,"escaped":"\u00e9","nullable":null}
	}`
	var indentedPlan bytes.Buffer
	require.NoError(t, json.Indent(&indentedPlan, []byte(serverPlan), "", "  "))
	indentedPlan.WriteByte('\n')

	fallbackPlan := `{"identifier":"my-plan","parallelism":2,"fallback":true,"tasks":{
		"0":{"node_number":0,"tests":[{"path":"apple"}]},
		"1":{"node_number":1,"tests":[{"path":"banana"}]}
	}}`

	for _, tt := range []struct {
		name       string
		envOnly    bool
		noPlanOut  bool
		cacheMiss  bool
		fallback   bool
		emptyBody  bool
		billing    bool
		node       int
		exitCode   int
		writeError string
	}{
		{name: "cached plan and flag overrides env"},
		{name: "environment variable", envOnly: true},
		{name: "freshly created plan", cacheMiss: true},
		{name: "other node writes entire plan", node: 1},
		{name: "cached error plan fallback", fallback: true},
		{name: "created error plan fallback", cacheMiss: true, fallback: true},
		{name: "empty GET fallback", emptyBody: true, fallback: true},
		{name: "empty POST fallback", cacheMiss: true, emptyBody: true, fallback: true},
		{name: "empty GET fallback without plan-out", emptyBody: true, fallback: true, noPlanOut: true},
		{name: "empty POST fallback without plan-out", cacheMiss: true, emptyBody: true, fallback: true, noPlanOut: true},
		{name: "API unavailable fallback", fallback: true, billing: true},
		{name: "test exit status preserved", exitCode: 7},
		{name: "cannot open destination", writeError: "opening --plan-out file"},
		{name: "cannot create parents", writeError: "creating --plan-out parent directories"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.New()
			t.Cleanup(func() { cfg = config.New() })
			dir := t.TempDir()
			planPath := filepath.Join(dir, "nested", "plan.json")
			envPath := filepath.Join(dir, "env-plan.json")
			if tt.envOnly {
				planPath = envPath
			}
			if tt.writeError == "opening --plan-out file" {
				planPath = dir
			} else if tt.writeError != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "nested"), nil, 0o600))
			}
			t.Setenv("BUILDKITE_TEST_ENGINE_PLAN_OUT", envPath)
			targetsPath := filepath.Join(dir, "targets.txt")
			require.NoError(t, os.WriteFile(targetsPath, []byte("apple\nbanana\n"), 0o600))
			ranPath := filepath.Join(dir, "ran.txt")
			// Prove the file exists before tests start, and record the node's runnable target.
			planCheck := `test -s "$1" || exit 99;`
			if tt.noPlanOut {
				t.Setenv("BUILDKITE_TEST_ENGINE_PLAN_OUT", "")
				planCheck = ""
			}
			testCommand := fmt.Sprintf(`sh -c '%s printf "%%s" "$3" > "$2"; exit %d' sh %q %q {{testExamples}}`, planCheck, tt.exitCode, planPath, ranPath)

			planRequests := 0
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/analytics/organizations/org/suites/suite/test_plan" {
					fmt.Fprint(w, `{}`) // Existing post-run metadata request.
					return
				}
				planRequests++
				switch {
				case tt.billing:
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"message":"Billing Error: please update your plan"}`)
				case tt.cacheMiss && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"message":"not found"}`)
				case tt.emptyBody:
					w.WriteHeader(http.StatusOK)
				case tt.fallback:
					fmt.Fprint(w, `{"tasks":{}}`)
				default:
					fmt.Fprint(w, serverPlan)
				}
			}))
			defer svr.Close()

			cmd := &cli.Command{
				Name: "bktec",
				Commands: []*cli.Command{{
					Name: "run", Flags: runCommandFlags(),
					Action: func(ctx context.Context, cmd *cli.Command) error {
						return command.Run(ctx, &cfg, cmd.String("files"))
					},
				}},
			}
			args := []string{"bktec", "run", "--files", targetsPath,
				"--test-runner", "custom", "--test-file-pattern", "*", "--test-command", testCommand,
				"--organization-slug", "org", "--suite-slug", "suite", "--base-url", svr.URL,
				"--plan-identifier", "my-plan", "--parallelism", "2", "--parallel-job", strconv.Itoa(tt.node),
				"--location-prefix", "app/"}
			if !tt.envOnly && !tt.noPlanOut {
				args = append(args, "--plan-out", planPath)
			}
			err := cmd.Run(context.Background(), args)
			if tt.writeError != "" {
				require.ErrorContains(t, err, tt.writeError)
				assert.Contains(t, err.Error(), planPath)
				assert.NoFileExists(t, ranPath)
				return
			}
			if tt.exitCode != 0 {
				var exitErr *exec.ExitError
				require.True(t, errors.As(err, &exitErr), "error = %v", err)
				assert.Equal(t, tt.exitCode, exitErr.ExitCode())
			} else {
				require.NoError(t, err)
			}
			if tt.noPlanOut {
				assert.NoFileExists(t, planPath)
			} else {
				contents, err := os.ReadFile(planPath)
				require.NoError(t, err)
				if tt.fallback {
					assert.JSONEq(t, fallbackPlan, string(contents))
				} else {
					assert.Equal(t, indentedPlan.String(), string(contents))
				}
			}
			ran, err := os.ReadFile(ranPath)
			require.NoError(t, err)
			assert.Equal(t, []string{"apple", "banana"}[tt.node], string(ran))
			if !tt.envOnly {
				assert.NoFileExists(t, envPath)
			}
			wantRequests := 1
			if tt.cacheMiss {
				wantRequests = 2
			}
			assert.Equal(t, wantRequests, planRequests, "no extra plan requests")
		})
	}
}

// TestSelectionFlagsVisibility checks that every planning command registers the
// selection flags and shows them in help.
func TestSelectionFlagsVisibility(t *testing.T) {
	visible := map[string]bool{
		"selection-strategy": true, "selection-param": true, "collect-git-metadata": true,
		"remote": true, "metadata": true,
	}
	commands := map[string]func() []cli.Flag{
		"run": runCommandFlags, "plan": planCommandFlags,
		"pool plan": poolPlanCommandFlags, "pool exec": poolExecCommandFlags,
	}
	for command, flags := range commands {
		byName := map[string]cli.Flag{}
		for _, f := range flags() {
			for _, n := range f.Names() {
				byName[n] = f
			}
		}
		for name, wantVisible := range visible {
			f, ok := byName[name]
			if !ok {
				t.Errorf("%s: missing --%s", command, name)
				continue
			}
			if got := f.(cli.VisibleFlag).IsVisible(); got != wantVisible {
				t.Errorf("%s: --%s visible = %v, want %v", command, name, got, wantVisible)
			}
		}
	}
}

func TestToolsCommandIsHidden(t *testing.T) {
	for _, c := range cliCommand.Commands {
		if c.Name == "tools" {
			assert.True(t, c.Hidden, "tools command should be hidden from help")
			return
		}
	}
	t.Fatal("cliCommand missing the tools subcommand")
}

func TestPlanCommandIncludesParallelismFlag(t *testing.T) {
	if !hasFlag(planCommandFlags(), "parallelism") {
		t.Fatalf("planCommandFlags() missing --parallelism flag; BUILDKITE_PARALLEL_JOB_COUNT will not be bound to cfg.Parallelism for `bktec plan`, breaking split-by-example slow-file detection")
	}
}

func TestOTLPRelayFlagsAreRunOnly(t *testing.T) {
	for _, name := range []string{"otlp-relay", "otlp-relay-upstream-endpoint"} {
		if !hasFlag(runCommandFlags(), name) {
			t.Errorf("runCommandFlags() missing --%s", name)
		}
		if hasFlag(planCommandFlags(), name) {
			t.Errorf("planCommandFlags() unexpectedly includes --%s", name)
		}
	}
}

func TestRunCommandDefaultsParallelismToOne(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })

	parallelism, wasSet := os.LookupEnv("BUILDKITE_PARALLEL_JOB_COUNT")
	if err := os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT"); err != nil {
		t.Fatalf("os.Unsetenv(BUILDKITE_PARALLEL_JOB_COUNT) error = %v", err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", parallelism)
		} else {
			_ = os.Unsetenv("BUILDKITE_PARALLEL_JOB_COUNT")
		}
	})

	cmd := &cli.Command{
		Name: "bktec",
		Commands: []*cli.Command{
			{
				Name:   "run",
				Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
				Flags:  runCommandFlags(),
			},
		},
	}

	if err := cmd.Run(context.Background(), []string{"bktec", "run"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Parallelism != 1 {
		t.Errorf("cfg.Parallelism = %d, want 1", cfg.Parallelism)
	}
}

func TestPlanCommandIncludesPlanIdentifierFlag(t *testing.T) {
	if !hasFlag(planCommandFlags(), "plan-identifier") {
		t.Fatalf("planCommandFlags() missing --plan-identifier flag; an off-agent `bktec plan` cannot set the plan cache key or skip the BUILDKITE_BUILD_ID/BUILDKITE_STEP_ID guards")
	}
}

func TestSelectorSplittingCompatibilityFlagIsAccepted(t *testing.T) {
	for _, command := range []string{"run", "plan"} {
		for _, suffix := range []string{"", "=true", "=false"} {
			t.Run(command+"/"+suffix, func(t *testing.T) {
				cfg = config.New()
				t.Cleanup(func() { cfg = config.New() })
				flags := runCommandFlags()
				if command == "plan" {
					flags = planCommandFlags()
				}

				cmd := &cli.Command{
					Name: "bktec",
					Commands: []*cli.Command{
						{
							Name:   command,
							Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
							Flags:  flags,
						},
					},
				}

				args := []string{"bktec", command, "--selector-splitting" + suffix}
				if err := cmd.Run(context.Background(), args); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	}
}

// --plan-out is registered in the plan command's mutually-exclusive PLAN
// OUTPUT group, so it is a valid output mode and cannot be combined with --json
// or --pipeline-upload.
func TestPlanCommandIncludesPlanOutOutputFlag(t *testing.T) {
	var planCmd *cli.Command
	for _, c := range cliCommand.Commands {
		if c.Name == "plan" {
			planCmd = c
		}
	}
	if planCmd == nil {
		t.Fatal("cliCommand missing the plan subcommand")
	}

	found := false
	for _, group := range planCmd.MutuallyExclusiveFlags {
		if group.Category != "PLAN OUTPUT" {
			continue
		}
		for _, flags := range group.Flags {
			if hasFlag(flags, "plan-out") {
				found = true
			}
		}
	}

	if !found {
		t.Fatal("plan command's PLAN OUTPUT group missing --plan-out")
	}
}

// TestCollectGitMetadataResolution checks that an explicit false, from either
// the flag or the env var, stays distinct from unset on every planning command.
func TestCollectGitMetadataResolution(t *testing.T) {
	commands := map[string]func() []cli.Flag{
		"run": runCommandFlags, "plan": planCommandFlags,
		"pool plan": poolPlanCommandFlags, "pool exec": poolExecCommandFlags,
	}
	for _, tc := range []struct {
		name, env string
		args      []string
		want      *bool
	}{
		{name: "unset"},
		{name: "flag true", args: []string{"--collect-git-metadata"}, want: new(true)},
		{name: "flag false", args: []string{"--collect-git-metadata=false"}, want: new(false)},
		{name: "env true", env: "true", want: new(true)},
		{name: "env false", env: "false", want: new(false)},
		{name: "flag overrides env", env: "true", args: []string{"--collect-git-metadata=false"}, want: new(false)},
	} {
		for name, flags := range commands {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				cfg = config.New()
				t.Cleanup(func() { cfg = config.New() })
				t.Setenv("BUILDKITE_TEST_ENGINE_COLLECT_GIT_METADATA", tc.env)
				cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{
					Name: "sub", Flags: flags(),
					Action: func(_ context.Context, cmd *cli.Command) error { return applyPlanRequestContext(cmd) },
				}}}
				require.NoError(t, cmd.Run(context.Background(), append([]string{"bktec", "sub"}, tc.args...)))
				assert.Equal(t, tc.want, cfg.CollectGitMetadata)
			})
		}
	}
}

// TestCollectGitMetadataOptOutRequestBody checks the wire request when a
// strategy is set but collection is opted out: only --metadata is sent, for
// run, plan followed by a cached run, and pool plan.
func TestCollectGitMetadataOptOutRequestBody(t *testing.T) {
	for _, optOut := range []string{"flag", "env"} {
		for _, flow := range []string{"run", "plan then cached run", "pool plan"} {
			t.Run(optOut+"/"+flow, func(t *testing.T) {
				t.Setenv("BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY", "manual")
				t.Setenv("BUILDKITE_TEST_ENGINE_COLLECT_GIT_METADATA", "")
				t.Setenv("BUILDKITE_TEST_ENGINE_POOL_ID", "")
				optOutArgs := []string{"--collect-git-metadata=false"}
				if optOut == "env" {
					t.Setenv("BUILDKITE_TEST_ENGINE_COLLECT_GIT_METADATA", "false")
					optOutArgs = nil
				}

				var posts []json.RawMessage
				var cached string
				svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/filter_tests"):
						fmt.Fprint(w, `{"tests":[]}`)
					case r.URL.Path == "/v2/organizations/org/test-scheduler/pools/plan":
						var req struct {
							Plan struct {
								Metadata json.RawMessage `json:"metadata"`
							} `json:"plan"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
						posts = append(posts, req.Plan.Metadata)
						w.WriteHeader(http.StatusAccepted)
						fmt.Fprint(w, `{"id":"pool-1","state":"planning"}`)
					case r.URL.Path == "/v2/analytics/organizations/org/suites/suite/test_plan" && r.Method == http.MethodGet:
						if cached == "" {
							w.WriteHeader(http.StatusNotFound)
							fmt.Fprint(w, `{"message":"not found"}`)
							return
						}
						fmt.Fprint(w, cached)
					case r.URL.Path == "/v2/analytics/organizations/org/suites/suite/test_plan":
						var req struct {
							Metadata json.RawMessage `json:"metadata"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
						posts = append(posts, req.Metadata)
						cached = `{"identifier":"my-plan","parallelism":1,"tasks":{"0":{"node_number":0,"tests":[{"format":"file","path":"spec/a_spec.rb"}]}}}`
						fmt.Fprint(w, cached)
					default:
						fmt.Fprint(w, `{}`) // Post-run metadata request.
					}
				}))
				defer svr.Close()

				files := filepath.Join(t.TempDir(), "files")
				require.NoError(t, os.WriteFile(files, []byte("spec/a_spec.rb\n"), 0o600))
				common := []string{"--files", files, "--organization-slug", "org", "--suite-slug", "suite",
					"--base-url", svr.URL, "--access-token", "token", "--metadata", "foo=bar"}

				invoke := func(name string, flags []cli.Flag, action func(context.Context) error, args ...string) {
					t.Helper()
					cfg = config.New()
					t.Cleanup(func() { cfg = config.New() })
					cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{
						Name: name, Flags: flags, DisableSliceFlagSeparator: true,
						Action: func(ctx context.Context, cmd *cli.Command) error {
							if err := applyPlanRequestContext(cmd); err != nil {
								return err
							}
							return action(ctx)
						},
					}}}
					args = append(append([]string{"bktec", name}, common...), args...)
					require.NoError(t, cmd.Run(context.Background(), append(args, optOutArgs...)))
				}
				runArgs := []string{"--plan-identifier", "my-plan", "--parallelism", "1",
					"--test-runner", "custom", "--test-file-pattern", "*", "--test-command", "true {{testExamples}}"}
				runTests := func(ctx context.Context) error { return command.Run(ctx, &cfg, files) }

				switch flow {
				case "run":
					invoke("run", runCommandFlags(), runTests, runArgs...)
				case "plan then cached run":
					invoke("plan", planCommandFlags(), func(ctx context.Context) error {
						return command.Plan(ctx, &cfg, files, command.PlanOutputJSON, "")
					}, runArgs...)
					require.NotEmpty(t, cached)
					invoke("run", runCommandFlags(), runTests, runArgs...)
				case "pool plan":
					invoke("plan", poolPlanCommandFlags(), func(ctx context.Context) error {
						return command.PoolPlan(ctx, &cfg, files, command.PlanOutputJSON, "")
					}, "--test-runner", "rspec", "--build-id", "build-1", "--pipeline-slug", "pipeline", "--pool-key", "rspec")
				}

				require.Len(t, posts, 1, "a cached run must not create another plan")
				assert.JSONEq(t, `{"foo":"bar"}`, string(posts[0]))
			})
		}
	}
}

// TestManualSelectionRequestBody checks that the selection strategy and params
// reach the test plan request unchanged for run, and for plan followed by a
// run that reuses the cached plan.
func TestManualSelectionRequestBody(t *testing.T) {
	for _, source := range []string{"flag", "env"} {
		for _, flow := range []string{"run", "plan then cached run"} {
			t.Run(source+"/"+flow, func(t *testing.T) {
				t.Setenv("BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY", "")
				t.Setenv("BUILDKITE_TEST_ENGINE_COLLECT_GIT_METADATA", "false")
				selectionArgs := []string{"--selection-strategy", "manual"}
				if source == "env" {
					t.Setenv("BUILDKITE_TEST_ENGINE_SELECTION_STRATEGY", "manual")
					selectionArgs = nil
				}
				selectionArgs = append(selectionArgs,
					"--selection-param", "files=spec/a_spec.rb\n spec/b_spec.rb\n",
					"--selection-param", "extra=a=b, c")

				var posts []json.RawMessage
				var cached string
				svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/filter_tests"):
						fmt.Fprint(w, `{"tests":[]}`)
					case r.URL.Path == "/v2/analytics/organizations/org/suites/suite/test_plan" && r.Method == http.MethodGet:
						if cached == "" {
							w.WriteHeader(http.StatusNotFound)
							fmt.Fprint(w, `{"message":"not found"}`)
							return
						}
						fmt.Fprint(w, cached)
					case r.URL.Path == "/v2/analytics/organizations/org/suites/suite/test_plan":
						var req struct {
							Selection json.RawMessage `json:"selection"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
						posts = append(posts, req.Selection)
						cached = `{"identifier":"my-plan","parallelism":1,"tasks":{"0":{"node_number":0,"tests":[{"format":"file","path":"spec/a_spec.rb"}]}},"selection":{"applied":true,"strategy":"manual","candidate_count":2,"selected_count":1}}`
						fmt.Fprint(w, cached)
					default:
						fmt.Fprint(w, `{}`) // Post-run metadata request.
					}
				}))
				defer svr.Close()

				files := filepath.Join(t.TempDir(), "files")
				require.NoError(t, os.WriteFile(files, []byte("spec/a_spec.rb\nspec/b_spec.rb\n"), 0o600))
				common := []string{"--files", files, "--organization-slug", "org", "--suite-slug", "suite",
					"--base-url", svr.URL, "--access-token", "token", "--plan-identifier", "my-plan", "--parallelism", "1",
					"--test-runner", "custom", "--test-file-pattern", "*", "--test-command", "true {{testExamples}}"}

				invoke := func(name string, flags []cli.Flag, action func(context.Context) error) {
					t.Helper()
					cfg = config.New()
					t.Cleanup(func() { cfg = config.New() })
					cmd := &cli.Command{Name: "bktec", Commands: []*cli.Command{{
						Name: name, Flags: flags, DisableSliceFlagSeparator: true,
						Action: func(ctx context.Context, cmd *cli.Command) error {
							if err := applyPlanRequestContext(cmd); err != nil {
								return err
							}
							return action(ctx)
						},
					}}}
					args := append(append([]string{"bktec", name}, common...), selectionArgs...)
					require.NoError(t, cmd.Run(context.Background(), args))
				}
				runTests := func(ctx context.Context) error { return command.Run(ctx, &cfg, files) }

				if flow == "plan then cached run" {
					invoke("plan", planCommandFlags(), func(ctx context.Context) error {
						return command.Plan(ctx, &cfg, files, command.PlanOutputJSON, "")
					})
					require.NotEmpty(t, cached)
				}
				invoke("run", runCommandFlags(), runTests)

				require.Len(t, posts, 1, "a cached run must not create another plan")
				assert.JSONEq(t, `{"strategy":"manual","params":{"files":"spec/a_spec.rb\n spec/b_spec.rb\n","extra":"a=b, c"}}`, string(posts[0]))
			})
		}
	}
}

func hasFlag(flags []cli.Flag, name string) bool {
	for _, flag := range flags {
		for _, n := range flag.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
}

// TestRunCommandEnvVarsBindToConfig verifies that every env var wired to a run
// command flag actually lands in the cfg struct. This guards against accidental
// removal of the Destination field from a flag definition.
func TestRunCommandEnvVarsBindToConfig(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })

	t.Setenv("BUILDKITE_ORGANIZATION_SLUG", "my-org")
	t.Setenv("BUILDKITE_BUILD_ID", "build-1")
	t.Setenv("BUILDKITE_JOB_ID", "job-2")
	t.Setenv("BUILDKITE_STEP_ID", "step-3")
	t.Setenv("BUILDKITE_BRANCH", "main")
	t.Setenv("BUILDKITE_RETRY_COUNT", "2")
	t.Setenv("BUILDKITE_PARALLEL_JOB", "1")
	t.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "4")
	t.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "access-token")
	t.Setenv("BUILDKITE_ANALYTICS_TOKEN", "upload-token")
	t.Setenv("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "my-suite")
	t.Setenv("BUILDKITE_TEST_ENGINE_BASE_URL", "https://example.com")
	t.Setenv("BUILDKITE_TEST_ENGINE_TAG_FILTERS", "fast")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_CMD", "go test ./...")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN", "**/*_test.go")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN", "vendor/**")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "gotest")
	t.Setenv("BUILDKITE_TEST_ENGINE_RESULT_PATH", "/tmp/results.json")
	t.Setenv("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_SELECTOR_FILE", "selectors.txt")
	t.Setenv("BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_LOCATION_PREFIX", "app/")
	t.Setenv("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "3")
	t.Setenv("BUILDKITE_TEST_ENGINE_DISABLE_RETRY_FOR_MUTED_TEST", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_RETRY_CMD", "go test -run .")
	t.Setenv("BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER", "my-plan")
	t.Setenv("BUILDKITE_TEST_ENGINE_DEBUG_ENABLED", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_OIDC", "false")
	t.Setenv("BUILDKITE_TEST_ENGINE_OIDC_LIFETIME", "1h")
	t.Setenv("BUILDKITE_TESTS_OTLP_RELAY", "true")
	t.Setenv("BUILDKITE_TESTS_OTLP_RELAY_UPSTREAM_ENDPOINT", "https://otlp.example/v1/traces")
	t.Setenv("BUILDKITE_TEST_ENGINE_TAGS", "env=production,region=us-east-1")

	cmd := &cli.Command{
		Name:  "bktec",
		Flags: freshFlags([]cli.Flag{debugFlag}),
		Commands: []*cli.Command{
			{
				Name:                      "run",
				DisableSliceFlagSeparator: true,
				Action:                    func(ctx context.Context, cmd *cli.Command) error { return nil },
				Flags:                     runCommandFlags(),
			},
		},
	}

	if err := cmd.Run(context.Background(), []string{"bktec", "run"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"OrganizationSlug", cfg.OrganizationSlug, "my-org"},
		{"BuildID", cfg.BuildID, "build-1"},
		{"JobID", cfg.JobID, "job-2"},
		{"StepID", cfg.StepID, "step-3"},
		{"Branch", cfg.Branch, "main"},
		{"JobRetryCount", cfg.JobRetryCount, 2},
		{"NodeIndex", cfg.NodeIndex, 1},
		{"Parallelism", cfg.Parallelism, 4},
		{"AccessToken", cfg.AccessToken, "access-token"},
		{"UploadToken", cfg.UploadToken, "upload-token"},
		{"SuiteSlug", cfg.SuiteSlug, "my-suite"},
		{"ServerBaseURL", cfg.ServerBaseURL, "https://example.com"},
		{"TagFilters", cfg.TagFilters, "fast"},
		{"TestCommand", cfg.TestCommand, "go test ./..."},
		{"TestFilePattern", cfg.TestFilePattern, "**/*_test.go"},
		{"TestFileExcludePattern", cfg.TestFileExcludePattern, "vendor/**"},
		{"TestRunner", cfg.TestRunner, "gotest"},
		{"ResultPath", cfg.ResultPath, "/tmp/results.json"},
		{"SplitByExample", cfg.SplitByExample, true},
		{"SelectorListPath", cfg.SelectorListPath, "selectors.txt"},
		{"FailOnNoTests", cfg.FailOnNoTests, true},
		{"LocationPrefix", cfg.LocationPrefix, "app/"},
		{"MaxRetries", cfg.MaxRetries, 3},
		// DISABLE_RETRY_FOR_MUTED_TEST=true means RetryForMutedTest should be false (flag Action inverts the bool)
		{"RetryForMutedTest", cfg.RetryForMutedTest, false},
		{"RetryCommand", cfg.RetryCommand, "go test -run ."},
		{"Identifier", cfg.Identifier, "my-plan"},
		{"DebugEnabled", cfg.DebugEnabled, true},
		{"OIDC", cfg.OIDC, false},
		{"OIDCLifetime", cfg.OIDCLifetime, time.Hour},
		{"OTLPRelay", cfg.OTLPRelay, true},
		{"OTLPRelayUpstreamEndpoint", cfg.OTLPRelayUpstreamEndpoint, "https://otlp.example/v1/traces"},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("cfg.%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	wantUploadTags := map[string]string{"env": "production", "region": "us-east-1"}
	if !reflect.DeepEqual(cfg.UploadTags, wantUploadTags) {
		t.Errorf("cfg.UploadTags = %v, want %v", cfg.UploadTags, wantUploadTags)
	}
}

// TestRunCommandFlagsDoNotShareParseState guards against the order-dependent
// failure from TE-6257: runCommandFlags() must hand out fresh flag instances so
// that explicitly setting a flag on one command does not leave urfave/cli's
// hasBeenSet state on a shared flag object, which would make a later command
// ignore the flag's env var source.
func TestRunCommandFlagsDoNotShareParseState(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })

	// First command parses --split-by-example on the CLI.
	first := &cli.Command{
		Name: "bktec",
		Commands: []*cli.Command{
			{
				Name:   "run",
				Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
				Flags:  runCommandFlags(),
			},
		},
	}
	if err := first.Run(context.Background(), []string{"bktec", "run", "--split-by-example"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second command, built independently, relies on the env var source. If the
	// two commands shared the same flag instance, the flag's hasBeenSet state
	// from the first parse would suppress the env var here.
	cfg = config.New()
	t.Setenv("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE", "true")
	second := &cli.Command{
		Name: "bktec",
		Commands: []*cli.Command{
			{
				Name:   "run",
				Action: func(ctx context.Context, cmd *cli.Command) error { return nil },
				Flags:  runCommandFlags(),
			},
		},
	}
	if err := second.Run(context.Background(), []string{"bktec", "run"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.SplitByExample {
		t.Fatalf("cfg.SplitByExample = false, want true; flag parse state leaked between commands")
	}
}

// TestCommandFlagsAllReturnFreshInstances asserts every flag handed out by the
// run and plan helpers is a distinct instance, i.e. freshFlag matched a concrete
// case rather than falling through to its default (which returns the shared
// global unchanged). This catches a new flag type being added without a
// corresponding freshFlag case, which would silently reintroduce the TE-6257
// parse-state leak for that flag.
func TestCommandFlagsAllReturnFreshInstances(t *testing.T) {

	for _, tc := range []struct {
		name  string
		flags []cli.Flag
	}{
		{"run", runCommandFlags()},
		{"plan", planCommandFlags()},
	} {
		for _, f := range tc.flags {
			if freshFlag(f) == f {
				t.Errorf("%s: freshFlag(%v) returned the shared global (type %T); add a case to freshFlag so its parse state is isolated", tc.name, f.Names(), f)
			}
		}
	}
}
