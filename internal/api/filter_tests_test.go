package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestFilterTests_SlowFiles(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestEngineClient",
		Provider: "TestEngineServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	cfg := config.New()
	cfg.Parallelism = 3
	cfg.SplitByExample = true

	params := FilterTestsParams{
		Files: []plan.TestCase{
			{
				Path: "./cat_spec.rb",
			},
			{
				Path: "./dog_spec.rb",
			},
			{
				Path: "./turtle_spec.rb",
			},
		},
		Env: &cfg,
	}

	err = mockProvider.
		AddInteraction().
		Given("A slow file exists").
		UponReceiving("A request to filter tests").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan/filter_tests", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.Like("Bearer asdf1234"))
			b.JSONBody(params)
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"tests": matchers.EachLike(matchers.MapMatcher{
					"path": matchers.Like("./turtle_spec.rb"),
				}, 1),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			c := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})
			got, err := c.FilterTests(context.Background(), "rspec", params)
			if err != nil {
				t.Errorf("FilterTests() error = %v", err)
			}
			want := []FilteredTest{
				{
					Path: "./turtle_spec.rb",
				},
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("FilterTests() diff (-got +want):\n%s", diff)
			}
			return nil
		})

	if err != nil {
		t.Error(err)
	}
}

func TestFilterTests_InternalServerError(t *testing.T) {
	originalTimeout := retryTimeout
	retryTimeout = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = originalTimeout
	})

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "something went wrong"}`, http.StatusInternalServerError)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "msy-org",
		ServerBaseUrl:    svr.URL,
	})

	_, err := c.FilterTests(context.Background(), "my-suite", FilterTestsParams{
		Files: []plan.TestCase{},
	})

	if !errors.Is(err, ErrRetryTimeout) {
		t.Errorf("FilterTests() error = %v, want %v", err, ErrRetryTimeout)
	}
}
