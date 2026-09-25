package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePoolConcurrentWorkers(t *testing.T) {
	var mu sync.Mutex
	var accepted []byte
	requests, filters := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/filter_tests"):
			filters++
			files := []api.TestPlanFile{{Path: "./testdata/rspec/spec/fruits/banana_spec.rb"}, {Path: "./testdata/rspec/spec/fruits/fig_spec.rb"}}
			if filters%2 == 0 {
				files[0], files[1] = files[1], files[0]
			}
			json.NewEncoder(w).Encode(map[string]any{"tests": files})
		case r.URL.Path == "/v2/organizations/buildkite/test-scheduler/pools/plan":
			requests++
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			if accepted == nil {
				accepted = body
			} else if !bytes.Equal(accepted, body) {
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"message":"different planning request"}`)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"id":"shared-pool","state":"planning"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := api.NewClient(api.ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "buildkite"})
	files := []string{"testdata/rspec/spec/fruits/fig_spec.rb", "testdata/rspec/spec/fruits/cherry_spec.rb", "testdata/rspec/spec/fruits/banana_spec.rb", "testdata/rspec/spec/fruits/apple_spec.rb"}
	var workers sync.WaitGroup
	for i := range 2 {
		list := filepath.Join(t.TempDir(), "files")
		require.NoError(t, os.WriteFile(list, []byte(strings.Join(files, "\n")), 0600))
		files[0], files[3] = files[3], files[0]
		workers.Go(func() {
			cfg := getConfig()
			cfg.PipelineSlug, cfg.PoolKey = "pipeline", "rspec"
			cfg.MaxRetries = i + 4 // Neither retries nor node identity belongs in the planning fingerprint.
			cfg.NodeIndex = i
			cfg.TestCommand = "rspec {{testExamples}}"
			pool, err := ResolvePool(context.Background(), cfg, list, client)
			assert.NoError(t, err)
			assert.Equal(t, "shared-pool", pool.ID)
			assert.Equal(t, "planning", pool.State)
		})
	}
	workers.Wait()
	require.Equal(t, 2, requests)
	require.Equal(t, 2, filters)
	require.JSONEq(t, `{
		"suite":"rspec","pipeline":"pipeline","build_id":"123","key":"rspec",
		"plan":{"runner":"rspec","branch":"tet-123-add-branch-name","tests":{
			"selectors":[{"value":"testdata/rspec/spec/fruits/apple_spec.rb"},{"value":"testdata/rspec/spec/fruits/cherry_spec.rb"}],
			"examples":[
				{"identifier":"./testdata/rspec/spec/fruits/banana_spec.rb[1:1]","name":"is yellow","path":"./testdata/rspec/spec/fruits/banana_spec.rb[1:1]","scope":"Banana"},
				{"identifier":"./testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]","name":"is green","path":"./testdata/rspec/spec/fruits/banana_spec.rb[1:2:1]","scope":"Banana when not ripe"},
				{"identifier":"./testdata/rspec/spec/fruits/fig_spec.rb[1:1]","name":"is purple","path":"./testdata/rspec/spec/fruits/fig_spec.rb[1:1]","scope":"Fig"}
			]
		}}
	}`, string(accepted))
}

func TestResolvePoolLeaseOptions(t *testing.T) {
	for _, tc := range []struct {
		name, lease             string
		durationMS, maxAttempts int
	}{
		{name: "server defaults"},
		{name: "duration only", durationMS: 60_000, lease: `{"costs":{"duration_p90_ms":60000}}`},
		{name: "max attempts only uses server cost default", maxAttempts: 25, lease: `{"max_attempts":25}`},
		{name: "both", durationMS: 45_000, maxAttempts: 12, lease: `{"costs":{"duration_p90_ms":45000},"max_attempts":12}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/filter_tests") {
					io.WriteString(w, `{"tests":[]}`)
					return
				}
				require.Equal(t, "/v2/organizations/buildkite/test-scheduler/pools/plan", r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				w.WriteHeader(http.StatusAccepted)
				io.WriteString(w, `{"id":"pool-1","state":"planning"}`)
			}))
			defer server.Close()
			cfg := getConfig()
			cfg.ServerBaseURL, cfg.PipelineSlug, cfg.PoolKey = server.URL, "pipeline", "rspec"
			cfg.PoolLeaseDurationMS, cfg.PoolLeaseMaxAttempts = tc.durationMS, tc.maxAttempts
			cfg.MaxRetries = 8
			client := api.NewClient(api.ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: cfg.OrganizationSlug})
			_, err := ResolvePool(context.Background(), cfg, "", client)
			require.NoError(t, err)
			if tc.lease == "" {
				require.NotContains(t, received, "lease")
			} else {
				require.JSONEq(t, tc.lease, string(received["lease"]))
			}
			require.NotContains(t, received, "attempt_policy", "local retries must not become Scheduler attempt policy")
		})
	}
}

func TestPoolPlanOutputsWithoutWaiting(t *testing.T) {
	for _, output := range []PlanOutput{PlanOutputJSON, PlanOutputPipelineUpload} {
		t.Run(fmt.Sprintf("output=%v", output), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/filter_tests") {
					io.WriteString(w, `{"tests":[]}`)
					return
				}
				requests++
				assert.Equal(t, "POST", r.Method, "plan must not poll for readiness")
				w.WriteHeader(202)
				io.WriteString(w, `{"id":"existing","state":"planning"}`)
			}))
			defer server.Close()
			cfg := getConfig()
			cfg.ServerBaseURL = server.URL
			cfg.PipelineSlug, cfg.PoolKey = "pipeline", "rspec"
			var buf bytes.Buffer
			setPlanWriter(t, &buf)
			// Observe the actual child environment and template argument.
			t.Setenv("BUILDKITE_TEST_ENGINE_POOL_ID", "stale-parent-id")
			setPipelineUploadCommand(t, "sh", "-c", `printf '%s:%s' "$BUILDKITE_TEST_ENGINE_POOL_ID" "$0"`)
			require.NoError(t, PoolPlan(context.Background(), cfg, "", output, "pipeline.yml"))
			require.Equal(t, 1, requests)
			if output == PlanOutputJSON {
				require.JSONEq(t, `{"BUILDKITE_TEST_ENGINE_POOL_ID":"existing"}`, buf.String())
			} else {
				require.Equal(t, "existing:pipeline.yml", buf.String())
			}
		})
	}
}

func TestPoolPlanRejectsExistingIDBeforeDiscovery(t *testing.T) {
	err := PoolPlan(context.Background(), &config.Config{PoolID: "existing"}, "missing-file", PlanOutputJSON, "")
	require.ErrorContains(t, err, "pass the ID to pool exec")
}

func TestResolvePrecreatedPoolSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v2/organizations/acme/test-scheduler/pools/ready", r.URL.Path)
		io.WriteString(w, `{"id":"ready","state":"consuming","muted_tests":[{"scope":"Fruit","name":"is yellow","path":"banana_spec.rb:4"}]}`)
	}))
	defer server.Close()
	client := api.NewClient(api.ClientConfig{ServerBaseURL: server.URL, OrganizationSlug: "acme"})
	pool, err := ResolvePool(context.Background(), &config.Config{
		PoolID: "ready", PoolLeaseDurationMS: 60_000, PoolLeaseMaxAttempts: 25,
	}, "does-not-exist", client)
	require.NoError(t, err)
	ready, err := client.WaitForPool(context.Background(), pool.ID)
	require.NoError(t, err)
	require.Len(t, ready.MutedTests, 1)
	require.Equal(t, "Fruit", ready.MutedTests[0].Scope)
	require.Equal(t, "is yellow", ready.MutedTests[0].Name)
	require.Equal(t, "banana_spec.rb:4", ready.MutedTests[0].Path)
}
