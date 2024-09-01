package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestFilterTests_SlowFiles(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestSplitterClient",
		Provider: "TestSplitterServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

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
		Parallelism:    3,
		SplitByExample: true,
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
					"path":   matchers.Like("./turtle_spec.rb"),
					"reason": matchers.Like("slow_files"),
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
					Path:   "./turtle_spec.rb",
					Reason: "slow_files",
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

// func TestFetchFilesTiming_BadRequest(t *testing.T) {
// 	requestCount := 0
// 	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		requestCount++
// 		http.Error(w, `{"message": "bad request"}`, http.StatusBadRequest)
// 	}))
// 	defer svr.Close()

// 	c := NewClient(ClientConfig{
// 		OrganizationSlug: "my-org",
// 		ServerBaseUrl:    svr.URL,
// 	})

// 	files := []string{"apple_spec.rb", "banana_spec.rb"}
// 	_, err := c.FetchFilesTiming(context.Background(), "my-suite", files)

// 	if requestCount > 1 {
// 		t.Errorf("http request count = %v, want  %d", requestCount, 1)
// 	}

// 	if err.Error() != "bad request" {
// 		t.Errorf("FetchFilesTiming() error = %v, want %v", err, ErrRetryTimeout)
// 	}
// }

// func TestFetchFilesTiming_InternalServerError(t *testing.T) {
// 	originalTimeout := retryTimeout
// 	retryTimeout = 1 * time.Millisecond
// 	t.Cleanup(func() {
// 		retryTimeout = originalTimeout
// 	})

// 	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		http.Error(w, `{"message": "something went wrong"}`, http.StatusInternalServerError)
// 	}))
// 	defer svr.Close()

// 	c := NewClient(ClientConfig{
// 		OrganizationSlug: "my-org",
// 		ServerBaseUrl:    svr.URL,
// 	})

// 	files := []string{"apple_spec.rb", "banana_spec.rb"}
// 	_, err := c.FetchFilesTiming(context.Background(), "my-suite", files)

// 	if !errors.Is(err, ErrRetryTimeout) {
// 		t.Errorf("FetchFilesTiming() error = %v, want %v", err, ErrRetryTimeout)
// 	}
// }
