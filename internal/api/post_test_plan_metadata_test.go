package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
)

func TestPostTestPlanMetadata(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestEngineClient",
		Provider: "TestEngineServer",
	})

	if err != nil {
		t.Fatal(err)
	}

	cfg := config.New()
	cfg.Parallelism = 3
	cfg.NodeIndex = 1
	cfg.SuiteSlug = "my_slug"
	cfg.Identifier = "abc123"

	params := TestPlanMetadataParams{
		Version: "0.7.0",
		Env:     cfg,
		Timeline: []Timeline{
			{
				Event:     "test_start",
				Timestamp: "2024-06-20T04:46:13.60977Z",
			},
			{
				Event:     "test_end",
				Timestamp: "2024-06-20T04:49:09.609793Z",
			},
		},
		Statistics: runner.RunStatistics{
			Total: 3,
		},
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan exists").
		UponReceiving("A request to post test plan metadata with identifier abc123").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan_metadata", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.String("Bearer asdf1234"))
			b.Header("Content-Type", matchers.String("application/json"))
			b.JSONBody(params)
		}).
		WillRespondWith(200, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"head": matchers.String("no_content"),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			c := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})

			_, err := c.DoWithRetry(context.Background(), httpRequest{
				Method: "POST",
				URL:    fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan_metadata", c.ServerBaseUrl, c.OrganizationSlug, "rspec"),
				Body:   params,
			}, nil)

			if err != nil {
				t.Errorf("PostTestPlanMetadata() error = %v", err)
			}

			return nil
		})

	if err != nil {
		t.Fatal(err)
	}
}

func TestPostTestPlanMetadata_NotFound(t *testing.T) {
	mockProvider, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: "TestEngineClient",
		Provider: "TestEngineServer",
	})

	if err != nil {
		t.Fatal(err)
	}

	cfg := config.New()
	cfg.Parallelism = 3
	cfg.NodeIndex = 1
	cfg.SuiteSlug = "my_slug"
	cfg.Identifier = "abc123"

	params := TestPlanMetadataParams{
		Version: "0.7.0",
		Env:     cfg,
		Timeline: []Timeline{
			{
				Event:     "test_start",
				Timestamp: "2024-06-20T04:46:13.60977Z",
			},
			{
				Event:     "test_end",
				Timestamp: "2024-06-20T04:49:09.609793Z",
			},
		},
		Statistics: runner.RunStatistics{
			Total: 3,
		},
	}

	err = mockProvider.
		AddInteraction().
		Given("A test plan doesn't exist").
		UponReceiving("A request to post test plan metadata with identifier abc123").
		WithRequest("POST", "/v2/analytics/organizations/buildkite/suites/rspec/test_plan_metadata", func(b *consumer.V2RequestBuilder) {
			b.Header("Authorization", matchers.String("Bearer asdf1234"))
			b.Header("Content-Type", matchers.String("application/json"))
			b.JSONBody(params)
		}).
		WillRespondWith(404, func(b *consumer.V2ResponseBuilder) {
			b.Header("Content-Type", matchers.Like("application/json; charset=utf-8"))
			b.JSONBody(matchers.MapMatcher{
				"message": matchers.Like("Test plan not found"),
			})
		}).
		ExecuteTest(t, func(config consumer.MockServerConfig) error {
			url := fmt.Sprintf("http://%s:%d", config.Host, config.Port)
			c := NewClient(ClientConfig{
				AccessToken:      "asdf1234",
				OrganizationSlug: "buildkite",
				ServerBaseUrl:    url,
			})

			_, err := c.DoWithRetry(context.Background(), httpRequest{
				Method: "POST",
				URL:    fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_plan_metadata", c.ServerBaseUrl, c.OrganizationSlug, "rspec"),
				Body:   params,
			}, nil)

			if err == nil {
				t.Errorf("PostTestPlanMetadata() error = %v, want %v", err, "Test plan not found")
			}

			return nil
		})

	if err != nil {
		t.Fatal(err)
	}
}
