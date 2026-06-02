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
