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
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestFetchTestPlan(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestSplitterClient",
		Provider: "TestSplitterServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan exists").
		UponReceiving("A request for test plan with identifier abc123").
		WithRequest("GET", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.Like("Bearer asdf1234"))
			b.Query("identifier", matchers.Like("abc123"))
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"tasks": matchers.Like(map[string]interface{}{
					"1": matchers.Like(map[string]interface{}{
						"node_number": matchers.Like(1),
						"tests": matchers.EachLike(matchers.MapMatcher{
							"path":               matchers.Like("sky_spec.rb:2"),
							"format":             matchers.Like("example"),
							"estimated_duration": matchers.Like(1000),
							"identifier":         matchers.Like("sky_spec.rb[1,1]"),
							"name":               matchers.Like("is blue"),
							"scope":              matchers.Like("sky"),
						}, 1),
					}),
				}),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)

			cfg := ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			}

			c := NewClient(cfg)

			got, err := c.FetchTestPlan(context.Background(), "rspec", "abc123")

			if err != nil {
				t.Errorf("FetchTestPlan() error = %v", err)
			}

			want := plan.TestPlan{
				Tasks: map[string]*plan.Task{
					"1": {
						NodeNumber: 1,
						Tests: []plan.TestCase{{
							Path:              "sky_spec.rb:2",
							Identifier:        "sky_spec.rb[1,1]",
							Name:              "is blue",
							Scope:             "sky",
							Format:            "example",
							EstimatedDuration: 1000,
						}},
					},
				},
			}

			if diff := cmp.Diff(got, &want); diff != "" {
				t.Errorf("FetchTestPlan() diff (-got +want):\n%s", diff)
			}

			return nil
		})

	if err != nil {
		t.Error("mockProvider error", err)
	}
}

func TestFetchTestPlan_NotFound(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestSplitterClient",
		Provider: "TestSplitterServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan doesn't exist").
		UponReceiving("A request for test plan with identifier abc123").
		WithRequest("GET", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan", func(b *consumer.V2RequestBuilder) {
			b.
				Header("Authorization", matchers.Like("Bearer asdf1234")).
				Query("identifier", matchers.Like("abc123"))
		}).
		WillRespondWith(404, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"message": matchers.Like("Not found"),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)

			cfg := ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			}

			c := NewClient(cfg)

			got, err := c.FetchTestPlan(context.Background(), "rspec", "abc123")

			if err != nil {
				t.Errorf("FetchTestPlan() error = %v", err)
			}

			if got != nil {
				t.Errorf("FetchTestPlan() = %v, want nil", got)
			}

			return nil
		})

	if err != nil {
		t.Error("mockProvider error", err)
	}
}

func TestFetchTestPlan_BadRequest(t *testing.T) {
	requestCount := 0
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	got, err := c.FetchTestPlan(context.Background(), "my-suite", "xyz")

	if requestCount > 1 {
		t.Errorf("http request count = %v, want %d", requestCount, 1)
	}

	if err.Error() != "bad request" {
		t.Errorf("FetchTestPlan() error = %v, want %v", err, "bad request")
	}

	if got != nil {
		t.Errorf("FetchTestPlan() = %v, want nil", got)
	}
}

func TestFetchTestPlan_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svr.Close()

	cfg := ClientConfig{
		AccessToken:      "asdf1234",
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	}

	c := NewClient(cfg)
	got, err := c.FetchTestPlan(context.Background(), "my-suite", "xyz")

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("FetchTestPlan() error = %v, want %v", err, ErrRetryTimeout)
	}

	if got != nil {
		t.Errorf("FetchTestPlan() = %v, want nil", got)
	}
}
