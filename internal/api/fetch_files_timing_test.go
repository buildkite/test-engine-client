package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestFetchFilesTiming(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestSplitterClient",
		Provider: "TestSplitterServer",
	})

	if err != nil {
		t.Error("Error mocking provider", err)
	}

	files := []string{"apple_spec.rb", "banana_spec.rb", "cherry_spec.rb", "dragonfruit_spec.rb"}

	err = mockProvider.
		AddInteraction().
		Given("Set of test files exists").
		UponReceiving("A request for test files").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_files", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.Like("Bearer asdf1234"))
			b.JSONBody(fetchFilesTimingParams{
				Paths: files,
			})
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"apple_spec.rb":  matchers.Like(1121),
				"banana_spec.rb": matchers.Like(3121),
				"cherry_spec.rb": matchers.Like(2143),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			c := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})
			got, err := c.FetchFilesTiming("rspec", files)
			if err != nil {
				t.Errorf("FetchFilesTiming() error = %v", err)
			}
			want := map[string]time.Duration{
				"apple_spec.rb":  1121 * time.Millisecond,
				"banana_spec.rb": 3121 * time.Millisecond,
				"cherry_spec.rb": 2143 * time.Millisecond,
			}
			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("FetchFilesTiming() diff (-got +want):\n%s", diff)
			}
			return nil
		})

	if err != nil {
		t.Error(err)
	}
}

func TestFetchFilesTiming_Error(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "something went wrong"}`, http.StatusInternalServerError)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	})

	files := []string{"apple_spec.rb", "banana_spec.rb"}
	_, err := c.FetchFilesTiming("my-suite", files)
	if err == nil {
		t.Errorf("FetchFilesTiming() error = %v, want an error", err)
	}

	want := "something went wrong"
	if got := err.Error(); got != want {
		t.Errorf("FetchFilesTiming() error = %v, want %v", got, want)
	}
}
