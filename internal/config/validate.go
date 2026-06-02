package config

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Checks common to all commands
func (c *Config) validate() error {
	if c.MaxRetries < 0 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "was %d, must be greater than or equal to 0", c.MaxRetries)
	}

	if c.Identifier == "" {
		if c.BuildID != "" && c.StepID != "" {
			c.Identifier = fmt.Sprintf("%s/%s", c.BuildID, c.StepID)
		} else {
			if c.BuildID == "" {
				c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
			}
			if c.StepID == "" {
				c.errs.appendFieldError("BUILDKITE_STEP_ID", "must not be blank")
			}
		}
	}

	if c.ServerBaseURL == "" {
		c.ServerBaseURL = "https://api.buildkite.com"
	} else {
		if _, err := url.ParseRequestURI(c.ServerBaseURL); err != nil {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_BASE_URL", "must be a valid URL")
		}
	}

	if c.AccessToken == "" {
		token, err := c.generateOIDCToken()

		if err != nil {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "%v", err)
		} else {
			c.AccessToken = token
			c.accessTokenIsOIDC = true
		}
	}

	if c.AccessToken == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "must not be blank")
	}

	if c.OrganizationSlug == "" {
		c.errs.appendFieldError("BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
	}

	if c.SuiteSlug == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "must not be blank")
	}

	runnersWithoutResultPath := map[string]bool{
		"cypress":      true,
		"pytest":       true,
		"pytest-pants": true,
		"custom":       true,
	}
	if c.ResultPath == "" && !runnersWithoutResultPath[c.TestRunner] {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}

	if c.TestRunner == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "must not be blank")
	}

	if c.TestRunner == "custom" {
		if c.TestCommand == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_CMD", "must not be blank when using the custom test runner")
		}
		if c.TestFilePattern == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN", "must not be blank when using the custom test runner")
		}
	}

	if c.TagFilters != "" && c.TestRunner != "pytest" {
		c.errs.appendFieldError(
			"BUILDKITE_TEST_ENGINE_TAG_FILTERS",
			"tag filtering is only supported for the pytest test runner",
		)
	}

	if c.SelectionStrategy == "" && len(c.SelectionParams) > 0 {
		c.errs.appendFieldError("selection-param", "selection strategy must be set when selection params are provided")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// Validation for the `bktec run` command
func (c *Config) ValidateForRun() error {
	_ = c.validate()

	// Upload token could come from the env BUILDKITE_ANALYTICS_TOKEN, but may be blank ...
	if c.UploadToken == "" {
		if c.accessTokenIsOIDC {
			// If OIDC was used to generate the bktec API access token then the same token
			// can be used for collector uploads.
			c.UploadToken = c.AccessToken
		} else {
			// If OIDC was *not* used to generate the bktec API access token then we need
			// to generate a token for collector uploads.
			token, err := c.generateOIDCToken()

			if err != nil {
				c.errs.appendFieldError("BUILDKITE_ANALYTICS_TOKEN", "%v", err)
			}
			c.UploadToken = token
		}
	}

	// The order of the range validation matters.
	// The range validation of BUILDKITE_PARALLEL_JOB depends on the result of BUILDKITE_PARALLEL_JOB_COUNT validation at the first step.
	// We need to validate the range of BUILDKITE_PARALLEL_JOB first before we add the range validation error to BUILDKITE_PARALLEL_JOB_COUNT.
	if c.errs["BUILDKITE_PARALLEL_JOB"] == nil {
		if got, min := c.NodeIndex, 0; got < 0 {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must be greater than or equal to %d", got, min)
		}

		if c.errs["BUILDKITE_PARALLEL_JOB_COUNT"] == nil {
			if got, max := c.NodeIndex, c.Parallelism-1; got > max {
				c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB", "was %d, must not be greater than %d", got, max)
			}
		}
	}

	if c.errs["BUILDKITE_PARALLEL_JOB_COUNT"] == nil {
		if got, min := c.Parallelism, 1; got < min {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must be greater than or equal to %d", got, min)
		}

		if got, max := c.Parallelism, 1000; got > max {
			c.errs.appendFieldError("BUILDKITE_PARALLEL_JOB_COUNT", "was %d, must not be greater than %d", got, max)
		}
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// ValidateForBackfillCommitMetadata validates config for the backfill-commit-metadata command.
// API connection fields and suite slug are required in all modes (the presigned upload
// endpoint is suite-scoped). Collection-only fields (days, concurrency) are checked when
// --upload is not set.
func (c *Config) ValidateForBackfillCommitMetadata() error {
	if c.ServerBaseURL == "" {
		c.ServerBaseURL = "https://api.buildkite.com"
	} else {
		if _, err := url.ParseRequestURI(c.ServerBaseURL); err != nil {
			c.errs.appendFieldError("--base-url / BUILDKITE_TEST_ENGINE_BASE_URL", "must be a valid URL")
		}
	}

	if c.OrganizationSlug == "" {
		c.errs.appendFieldError("--organization-slug / BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
	}

	// SuiteSlug is required in both modes: the presigned upload endpoint is
	// suite-scoped, so even upload-only needs the suite to construct the URL.
	if c.SuiteSlug == "" {
		c.errs.appendFieldError("--suite-slug / BUILDKITE_TEST_ENGINE_SUITE_SLUG", "must not be blank")
	}

	// OIDC fallback, mirrors validate(). Mint needs org and suite slug,
	// so the slug checks above must run first.
	if c.AccessToken == "" {
		token, err := c.generateOIDCToken()

		if err != nil {
			c.errs.appendFieldError("--access-token / BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "%v", err)
		} else {
			c.AccessToken = token
		}
	}

	if c.AccessToken == "" {
		c.errs.appendFieldError("--access-token / BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "must not be blank")
	}

	// Upload-only mode: skip days/concurrency checks (those govern collection).
	if c.UploadFile != "" {
		if len(c.errs) > 0 {
			return c.errs
		}
		return nil
	}

	if got, min := c.Days, 1; got < min {
		c.errs.appendFieldError("--days", "was %d, must be greater than or equal to %d", got, min)
	}

	if got, min := c.Concurrency, 1; got < min {
		c.errs.appendFieldError("--concurrency", "was %d, must be greater than or equal to %d", got, min)
	}

	if len(c.errs) > 0 {
		return c.errs
	}
	return nil
}

// Validation for the `bktec plan` command
func (c *Config) ValidateForPlan() error {
	_ = c.validate()

	if c.TargetTime != 0 {
		if c.TargetTime <= 0 {
			c.errs.appendFieldError("target-time", "was %s, must be greater than 0", c.TargetTime.String())
		}

		if c.TargetTime > time.Hour*24 {
			c.errs.appendFieldError("target-time", "was %s, must be less than or equal to 24 hours", c.TargetTime.String())
		}

		if c.MaxParallelism == 0 {
			c.errs.appendFieldError("max-parallelism", "must be set when target-time is set")
		}
	}

	if c.MaxParallelism != 0 {
		if c.MaxParallelism < 0 || c.MaxParallelism > 1000 {
			c.errs.appendFieldError("max-parallelism", "was %d, must be between 0 and 1000", c.MaxParallelism)
		}
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

func (c *Config) validateQueueCommon() error {
	c.validateQueueTransportAndAuth()

	if c.QueueName == "" {
		c.QueueName = c.QueueStepKey
	}
	if c.QueueName == "" {
		c.QueueName = "tests"
	}

	if c.QueueBatchSize == 0 {
		c.QueueBatchSize = 100
	}
	if c.QueuePushBatchSize == 0 {
		c.QueuePushBatchSize = 1000
	}
	if c.QueueLeaseSeconds == 0 {
		c.QueueLeaseSeconds = 600
	}
	if c.QueuePollSeconds == 0 {
		c.QueuePollSeconds = 5
	}

	if c.QueueBatchSize < 1 || c.QueueBatchSize > 1000 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_BATCH_SIZE", "was %d, must be between 1 and 1000", c.QueueBatchSize)
	}
	if c.QueuePushBatchSize < 1 || c.QueuePushBatchSize > 10000 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_PUSH_BATCH_SIZE", "was %d, must be between 1 and 10000", c.QueuePushBatchSize)
	}
	if c.QueueLeaseSeconds < 1 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_LEASE_SECONDS", "was %d, must be greater than 0", c.QueueLeaseSeconds)
	}
	if c.QueuePollSeconds < 0 {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_POLL_SECONDS", "was %d, must be greater than or equal to 0", c.QueuePollSeconds)
	}

	if c.QueueOrganizationUUID == "" && !queueOIDCIdentityAvailable(c) {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_ORGANIZATION_UUID", "must not be blank")
	}
	if c.QueueSuiteUUID == "" && !queueOIDCIdentityAvailable(c) {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_SUITE_UUID", "must not be blank")
	}
	if c.BuildID == "" {
		c.errs.appendFieldError("BUILDKITE_BUILD_ID", "must not be blank")
	}
	if c.JobID == "" {
		c.errs.appendFieldError("BUILDKITE_JOB_ID", "must not be blank")
	}
	if c.QueuePipelineSlug == "" {
		c.errs.appendFieldError("BUILDKITE_PIPELINE_SLUG", "must not be blank")
	}
	if c.TestRunner == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "must not be blank")
	}

	if c.TestRunner == "custom" {
		if c.TestCommand == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_CMD", "must not be blank when using the custom test runner")
		}
		if c.TestFilePattern == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN", "must not be blank when using the custom test runner")
		}
	}

	runnersWithoutResultPath := map[string]bool{
		"cypress":      true,
		"pytest":       true,
		"pytest-pants": true,
		"custom":       true,
	}
	if c.ResultPath == "" && !runnersWithoutResultPath[c.TestRunner] {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

func (c *Config) validateQueueTransportAndAuth() {
	if c.ServerBaseURL == "" {
		c.ServerBaseURL = "https://api.buildkite.com"
	}

	if c.QueueServerBaseURL == "" {
		c.QueueServerBaseURL = "http://127.0.0.1:9998"
	} else if _, err := url.ParseRequestURI(c.QueueServerBaseURL); err != nil {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_SERVER_URL", "must be a valid URL")
	}

	if c.QueueOIDCAudience == "" {
		if c.OIDC && c.QueueAccessToken == "" {
			if c.OrganizationSlug == "" {
				c.errs.appendFieldError("BUILDKITE_ORGANIZATION_SLUG", "must not be blank")
			}
			if c.SuiteSlug == "" {
				c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "must not be blank")
			}
		}
		c.QueueOIDCAudience = fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s/test_queue", c.ServerBaseURL, c.OrganizationSlug, c.SuiteSlug)
	}

	if c.QueueAccessToken == "" && c.OIDC && len(c.errs) == 0 {
		token, err := c.generateOIDCTokenForAudience(c.QueueOIDCAudience, true)
		if err != nil {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_ACCESS_TOKEN", "%v", err)
		} else {
			c.QueueAccessToken = token
		}
	}
}

// ValidateForQueueMetrics validates config for `bktec queue metrics`.
func (c *Config) ValidateForQueueMetrics() error {
	c.validateQueueTransportAndAuth()
	if c.QueueUUID == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_UUID", "must not be blank")
	} else if !validUUIDString(c.QueueUUID) {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_QUEUE_UUID", "must be a valid UUID")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// ValidateForQueuePush validates config for `bktec queue push`.
func (c *Config) ValidateForQueuePush() error {
	return c.validateQueueCommon()
}

// ValidateForQueueWorker validates config for `bktec queue worker`.
func (c *Config) ValidateForQueueWorker() error {
	return c.validateQueueCommon()
}

func validUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func queueOIDCIdentityAvailable(c *Config) bool {
	return c.OIDC
}

func (c *Config) generateOIDCToken() (token string, err error) {
	if !c.OIDC {
		return "", nil
	}

	suiteURL := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s", c.ServerBaseURL, c.OrganizationSlug, c.SuiteSlug)
	return c.generateOIDCTokenForAudience(suiteURL, false)
}

func (c *Config) generateOIDCTokenForAudience(audience string, includeBuildIDClaim bool) (token string, err error) {
	var tokenWriter strings.Builder
	var errorWriter strings.Builder
	lifetime := strconv.Itoa(int(c.OIDCLifetime.Seconds()))
	args := []string{"oidc", "request-token", "--audience", audience, "--lifetime", lifetime}
	if includeBuildIDClaim {
		args = append(args, "--claim", "build_id", "--claim", "organization_id")
	}
	// Skipping a security linter check here. The issue is "G204: Subprocess launched with a potential tainted input or cmd arguments"
	// Given that running tainted input commands is bktec's raison d'etre this is acceptable.
	cmd := exec.Command(c.BuildkiteAgentCommand, args...) //nolint:gosec
	cmd.Stderr = &errorWriter
	cmd.Stdout = &tokenWriter
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error generating token: %s: %v", errorWriter.String(), err)
	}

	return strings.TrimSpace(tokenWriter.String()), nil
}
