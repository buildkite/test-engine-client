package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/config"
)

// makeOIDCToken builds an unsigned JWT-shaped token whose payload contains the
// given claims. Only the payload segment is decoded by the client.
func makeOIDCToken(t *testing.T, claims map[string]string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling claims: %v", err)
	}
	return "fakeheader." + base64.RawURLEncoding.EncodeToString(payload) + ".fakesignature"
}

// fakeBuildkiteAgent writes a fake buildkite-agent script that prints the given
// token, and returns its path.
func fakeBuildkiteAgent(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-buildkite-agent")
	script := "#!/bin/sh\necho \"$FAKE_OIDC_TOKEN\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake buildkite-agent: %v", err)
	}
	t.Setenv("FAKE_OIDC_TOKEN", token)
	return path
}

func schedulerClaims(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"organization_id": "org-uuid",
		"pipeline_id":     "pipeline-uuid",
		"build_id":        "build-uuid",
		"job_id":          "job-uuid",
	}
}

func getSchedulerConfig(t *testing.T, claims map[string]string) *config.Config {
	t.Helper()
	cfg := getConfig()
	cfg.Identifier = "build-uuid/step-uuid"
	cfg.TestScheduler = true
	cfg.PoolName = "default"
	cfg.OIDC = true
	cfg.BuildkiteAgentCommand = fakeBuildkiteAgent(t, makeOIDCToken(t, claims))
	return cfg
}

func TestCreateSchedulerPool(t *testing.T) {
	planRequestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec":
			_, _ = io.WriteString(w, `{"id": "suite-uuid"}`)
		case "/v2/organizations/buildkite/test-scheduler/plan":
			planRequestCount++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding plan request body: %v", err)
			}
			want := map[string]any{
				"organization_id":      "org-uuid",
				"suite_id":             "suite-uuid",
				"pipeline_id":          "pipeline-uuid",
				"build_id":             "build-uuid",
				"key":                  "default",
				"test_plan_identifier": "build-uuid/step-uuid",
			}
			for key, wantValue := range want {
				if body[key] != wantValue {
					t.Errorf("plan request body[%q] = %v, want %v", key, body[key], wantValue)
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{
				"pool": {"id": "pool-uuid", "key": "default", "state": "consuming"},
				"uploaded_entries_count": 3
			}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getSchedulerConfig(t, schedulerClaims(t))
	cfg.ServerBaseURL = svr.URL

	readStderr := captureStderr(t)
	err := createSchedulerPool(context.Background(), cfg)
	stderr := readStderr()

	if err != nil {
		t.Fatalf("createSchedulerPool() error = %v", err)
	}
	if planRequestCount != 1 {
		t.Errorf("plan request count = %d, want 1", planRequestCount)
	}
	if want := "Created test pool pool-uuid (consuming) with 3 entries"; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want substring %q", stderr, want)
	}
}

func TestCreateSchedulerPool_ConflictResolvesExistingPool(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/v2/analytics/organizations/buildkite/suites/rspec":
			_, _ = io.WriteString(w, `{"id": "suite-uuid"}`)
		case "/v2/organizations/buildkite/test-scheduler/plan":
			http.Error(w, `{"message": "Pool already exists"}`, http.StatusConflict)
		case "/v2/organizations/buildkite/test-scheduler/pools":
			query := r.URL.Query()
			if got := query.Get("pipeline_id"); got != "pipeline-uuid" {
				t.Errorf("pipeline_id query = %q", got)
			}
			if got := query.Get("build_id"); got != "build-uuid" {
				t.Errorf("build_id query = %q", got)
			}
			if got := query.Get("key"); got != "default" {
				t.Errorf("key query = %q", got)
			}
			_, _ = io.WriteString(w, `{"pools": [{"id": "pool-uuid", "key": "default", "state": "consuming"}]}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer svr.Close()

	cfg := getSchedulerConfig(t, schedulerClaims(t))
	cfg.ServerBaseURL = svr.URL

	readStderr := captureStderr(t)
	err := createSchedulerPool(context.Background(), cfg)
	stderr := readStderr()

	if err != nil {
		t.Fatalf("createSchedulerPool() error = %v", err)
	}
	if want := "Using existing test pool pool-uuid (consuming)"; !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want substring %q", stderr, want)
	}
}

func TestCreateSchedulerPool_MissingClaims(t *testing.T) {
	cfg := getSchedulerConfig(t, map[string]string{
		"organization_id": "org-uuid",
		// pipeline_id, build_id and job_id claims are missing
	})

	err := createSchedulerPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("createSchedulerPool() error = nil, want missing claims error")
	}

	if want := "OIDC token is missing required claims"; !strings.Contains(err.Error(), want) {
		t.Errorf("createSchedulerPool() error = %q, want substring %q", err.Error(), want)
	}
}
