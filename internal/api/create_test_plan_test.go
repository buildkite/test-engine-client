package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func ptr[T any](t T) *T { return &t }

func TestCreateTestPlan(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
	"tasks": {
		"task_1": {
			"node_number": 1,
			"tests": {
				"cases": [
					{
						"path": "dummy.spec",
						"estimated_duration": 1000000
					}
				],
				"format": "junit"
			}
		}
	}
}`)
	}))
	defer svr.Close()

	ctx := context.Background()

	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseUrl: svr.URL,
	})
	got, err := apiClient.CreateTestPlan(ctx, "my-suite", params)
	if err != nil {
		t.Errorf("CreateTestPlan(ctx, %v) error = %v", params, err)
	}
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"task_1": {
				NodeNumber: 1,
				Tests: plan.Tests{
					Cases: []plan.TestCase{{
						Path:              "dummy.spec",
						EstimatedDuration: ptr(1_000_000),
					}},
					Format: "junit",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("CreateTestPlan(ctx, %v) diff (-got +want):\n%s", params, diff)
	}
}

func TestCreateTestPlan_Error4xx(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer svr.Close()

	ctx := context.Background()
	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseUrl: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan(ctx, %v) diff (-got +want):\n%s", params, diff)
	}

	if !errors.Is(err, errInvalidRequest) {
		t.Errorf("CreateTestPlan(ctx, %v) want %v got %v", params, errInvalidRequest, err)
	}
}

// Test the client keeps getting 5xx error until reached context deadline
func TestCreateTestPlan_Timeout(t *testing.T) {

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer svr.Close()

	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseUrl: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(fetchCtx, "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan(ctx, %v) diff (-got +want):\n%s", params, diff)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("FetchTestPlan(ctx, %v) want %v, got %v", params, context.DeadlineExceeded, err)
	}
}
