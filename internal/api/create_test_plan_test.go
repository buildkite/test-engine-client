package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestCreateTestPlan(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestEngineClient",
		Provider: "TestPlanServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	params := TestPlanParams{
		Runner:      Rspec,
		Branch:      "tet-123-add-branch-name",
		Identifier:  "abc123",
		Parallelism: 3,
		Tests: TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "sky_spec.rb"},
			},
		},
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan doesn't exist").
		UponReceiving("A request to create test plan with identifier abc123 and split by example disabled").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.String("Bearer asdf1234"))
			b.Header("Content-Type", matchers.String("application/json"))
			b.JSONBody(params)
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.String("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"tasks": matchers.Like(map[string]interface{}{
					"0": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(0),
						"tests": matchers.EachLike(matchers.MapMatcher{
							"path":               matchers.Like("sky_spec.rb"),
							"format":             matchers.Like("file"),
							"estimated_duration": matchers.Like(1000),
						}, 1),
					}),
					"1": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(1),
						"tests":       []plan.TestCase{},
					}),
					"2": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(2),
						"tests":       []plan.TestCase{},
					}),
				}),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			ctx := context.Background()
			fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()

			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			apiClient := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})

			got, err := apiClient.CreateTestPlan(fetchCtx, "rspec", params)
			if err != nil {
				t.Errorf("CreateTestPlan(ctx, %v) error = %v", params, err)
			}

			want := plan.TestPlan{
				Tasks: map[string]*plan.Task{
					"0": {
						NodeNumber: 0,
						Tests: []plan.TestCase{{
							Path:              "sky_spec.rb",
							Format:            "file",
							EstimatedDuration: 1000,
						}},
					},
					"1": {
						NodeNumber: 1,
						Tests:      []plan.TestCase{},
					},
					"2": {
						NodeNumber: 2,
						Tests:      []plan.TestCase{},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("CreateTestPlan(ctx, %v) diff (-got +want):\n%s", params, diff)
			}

			return nil
		})

	if err != nil {
		t.Error("mockProvider error", err)
	}
}

func TestCreateTestPlan_SplitByExample(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestEngineClient",
		Provider: "TestPlanServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	params := TestPlanParams{
		Identifier:  "abc123",
		Parallelism: 3,
		Tests: TestPlanParamsTest{
			Files: []plan.TestCase{
				{Path: "sky_spec.rb"},
			},
			Examples: []plan.TestCase{
				{
					Path:       "sea_spec.rb:4",
					Name:       "is blue",
					Scope:      "sea",
					Identifier: "sea_spec.rb[1,1]",
				},
			},
		},
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan doesn't exist").
		UponReceiving("A request to create test plan with identifier abc123 and split by example enabled").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.String("Bearer asdf1234"))
			b.Header("Content-Type", matchers.String("application/json"))
			b.JSONBody(params)
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.String("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"tasks": matchers.Like(map[string]interface{}{
					"0": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(0),
						"tests": matchers.EachLike(matchers.MapMatcher{
							"path":               matchers.Like("sea_spec.rb:4"),
							"name":               matchers.Like("is blue"),
							"scope":              matchers.Like("sea"),
							"identifier":         matchers.Like("sea_spec.rb[1,1]"),
							"format":             matchers.Like("example"),
							"estimated_duration": matchers.Like(1000),
						}, 1),
					}),
					"1": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(1),
						"tests": matchers.EachLike(matchers.MapMatcher{
							"path":               matchers.Like("sky_spec.rb"),
							"format":             matchers.Like("file"),
							"estimated_duration": matchers.Like(1000),
						}, 1),
					}),
					"2": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(2),
						"tests":       []plan.TestCase{},
					}),
				}),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			ctx := context.Background()
			fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
			defer cancel()

			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			apiClient := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})

			got, err := apiClient.CreateTestPlan(fetchCtx, "rspec", params)
			if err != nil {
				t.Errorf("CreateTestPlan(ctx, %v) error = %v", params, err)
			}

			want := plan.TestPlan{
				Tasks: map[string]*plan.Task{
					"0": {
						NodeNumber: 0,
						Tests: []plan.TestCase{{
							Path:              "sea_spec.rb:4",
							Name:              "is blue",
							Scope:             "sea",
							Identifier:        "sea_spec.rb[1,1]",
							Format:            "example",
							EstimatedDuration: 1000,
						}},
					},
					"1": {
						NodeNumber: 1,
						Tests: []plan.TestCase{{
							Path:              "sky_spec.rb",
							Format:            "file",
							EstimatedDuration: 1000,
						}},
					},
					"2": {
						NodeNumber: 2,
						Tests:      []plan.TestCase{},
					},
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("CreateTestPlan(ctx, %v) diff (-got +want):\n%s", params, diff)
			}

			return nil
		})

	if err != nil {
		t.Error("mockProvider error", err)
	}
}

func TestCreateTestPlan_BadRequest(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
	}))
	defer svr.Close()

	ctx := context.Background()
	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseUrl: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(ctx, "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if requestCount > 1 {
		t.Errorf("http request count = %v, want %d", requestCount, 1)
	}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}

	if err.Error() != "bad request" {
		t.Errorf("CreateTestPlan() error = %v, want %v", err, ErrRetryTimeout)
	}
}

func TestCreateTestPlan_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	params := TestPlanParams{}
	apiClient := NewClient(ClientConfig{
		ServerBaseUrl: svr.URL,
	})

	got, err := apiClient.CreateTestPlan(context.Background(), "my-suite", params)

	wantTestPlan := plan.TestPlan{}

	if diff := cmp.Diff(got, wantTestPlan); diff != "" {
		t.Errorf("CreateTestPlan() diff (-got +want):\n%s", diff)
	}

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("CreateTestPlan() want %v, got %v", ErrRetryTimeout, err)
	}
}
