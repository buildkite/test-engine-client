package config

import "testing"

func createQueueConfig() Config {
	c := createConfig()
	c.BuildID = "019e8713-0000-7000-8000-000000000001"
	c.JobID = "019e8713-0000-7000-8000-000000000002"
	c.QueueOrganizationUUID = "019e8713-0000-7000-8000-000000000010"
	c.QueueSuiteUUID = "019e8713-0000-7000-8000-000000000011"
	c.QueuePipelineSlug = "pipeline"
	c.QueueStepKey = "rspec"
	c.QueueUUID = "019e8713-0000-7000-8000-000000000020"
	c.OIDC = false
	return c
}

func TestValidateForQueuePushDefaults(t *testing.T) {
	c := createQueueConfig()

	if err := c.ValidateForQueuePush(); err != nil {
		t.Fatalf("ValidateForQueuePush() error = %v", err)
	}

	if c.QueueName != "rspec" {
		t.Fatalf("QueueName = %q, want rspec", c.QueueName)
	}
	if c.QueueServerBaseURL != "http://127.0.0.1:9998" {
		t.Fatalf("QueueServerBaseURL = %q, want local default", c.QueueServerBaseURL)
	}
	if c.QueueBatchSize != 100 || c.QueuePushBatchSize != 1000 || c.QueueLeaseSeconds != 600 || c.QueuePollSeconds != 5 {
		t.Fatalf("queue defaults = batch %d push %d lease %d poll %d", c.QueueBatchSize, c.QueuePushBatchSize, c.QueueLeaseSeconds, c.QueuePollSeconds)
	}
}

func TestValidateForQueuePushOIDC(t *testing.T) {
	c := createQueueConfig()
	c.OIDC = true
	c.QueueAccessToken = ""
	c.BuildkiteAgentCommand = "./mock-buildkite-agent"

	if err := c.ValidateForQueuePush(); err != nil {
		t.Fatalf("ValidateForQueuePush() error = %v", err)
	}
	if c.QueueAccessToken != "mocktoken" {
		t.Fatalf("QueueAccessToken = %q, want mocktoken", c.QueueAccessToken)
	}
}

func TestValidateForQueuePushOIDCDoesNotRequireOrganizationOrSuiteUUIDs(t *testing.T) {
	c := createQueueConfig()
	c.OIDC = true
	c.QueueAccessToken = ""
	c.QueueOrganizationUUID = ""
	c.QueueSuiteUUID = ""
	c.BuildkiteAgentCommand = "./mock-buildkite-agent"

	if err := c.ValidateForQueuePush(); err != nil {
		t.Fatalf("ValidateForQueuePush() error = %v", err)
	}
}

func TestValidateForQueuePushRequiresQueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = ""

	if err := c.ValidateForQueuePush(); err == nil {
		t.Fatalf("ValidateForQueuePush() error = nil, want queue UUID validation error")
	}
}

func TestValidateForQueuePushExplicitTokenStillRequiresUUIDsWithoutOIDCMode(t *testing.T) {
	c := createQueueConfig()
	c.OIDC = false
	c.QueueAccessToken = "token"
	c.QueueOrganizationUUID = ""
	c.QueueSuiteUUID = ""

	if err := c.ValidateForQueuePush(); err == nil {
		t.Fatalf("ValidateForQueuePush() error = nil, want UUID validation error")
	}
}

func TestValidateForQueueMetrics(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = "019e8713-0000-7000-8000-000000000020"
	c.TestRunner = ""
	c.ResultPath = ""

	if err := c.ValidateForQueueMetrics(); err != nil {
		t.Fatalf("ValidateForQueueMetrics() error = %v", err)
	}
}

func TestValidateForQueueMetricsRequiresQueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = ""

	if err := c.ValidateForQueueMetrics(); err == nil {
		t.Fatalf("ValidateForQueueMetrics() error = nil, want queue UUID validation error")
	}
}

func TestValidateForQueueMetricsRejectsInvalidQueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = "not-a-uuid"

	if err := c.ValidateForQueueMetrics(); err == nil {
		t.Fatalf("ValidateForQueueMetrics() error = nil, want invalid queue UUID error")
	}
}

func TestValidateForQueueMetricsRejectsNonV7QueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = "123e4567-e89b-12d3-a456-426614174000"

	if err := c.ValidateForQueueMetrics(); err == nil {
		t.Fatalf("ValidateForQueueMetrics() error = nil, want UUIDv7 validation error")
	}
}

func TestValidateForQueueWorkerRequiresQueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = ""

	if err := c.ValidateForQueueWorker(); err == nil {
		t.Fatalf("ValidateForQueueWorker() error = nil, want queue UUID validation error")
	}
}

func TestValidateForQueueWorkerAcceptsQueueUUID(t *testing.T) {
	c := createQueueConfig()
	c.QueueUUID = "019e8713-0000-7000-8000-000000000020"

	if err := c.ValidateForQueueWorker(); err != nil {
		t.Fatalf("ValidateForQueueWorker() error = %v", err)
	}
}
