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

// splitBySelectorList reports whether the run is splitting work using a
// user-provided selector list instead of discovered test files. In this mode
// test file discovery is skipped, so TestFilePattern isn't required.
func (c *Config) splitBySelectorList() bool {
	return c.SelectorSplitting && c.SelectorListPath != ""
}

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

	if c.TestRunner == "" {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "must not be blank")
	}

	if c.TestRunner == "custom" {
		if c.TestCommand == "" {
			c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_TEST_CMD", "must not be blank when using the custom test runner")
		}
		// A selector list replaces test file discovery, so the file pattern
		// isn't required when splitting by a provided selector list.
		if c.TestFilePattern == "" && !c.splitBySelectorList() {
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

	if c.SelectorListPath != "" && !c.SelectorSplitting {
		c.errs.appendFieldError("selectors", "selector splitting must be enabled when a selector list is provided")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

// Validation for the `bktec run` command
func (c *Config) ValidateForRun() error {
	_ = c.validate()

	// result-path is only consumed when running tests (command construction and
	// report parsing), so it is required here but not for `plan`. Checked as an
	// inclusion list of runners that need it (rather than excluding the ones that
	// don't), so an unrecognized runner value fails later with runner.DetectRunner's
	// more informative "invalid runner" error instead of this one.
	runnersWithResultPath := map[string]bool{
		"rspec":      true,
		"jest":       true,
		"playwright": true,
		"gotest":     true,
		"cucumber":   true,
	}
	if c.ResultPath == "" && runnersWithResultPath[c.TestRunner] {
		c.errs.appendFieldError("BUILDKITE_TEST_ENGINE_RESULT_PATH", "must not be blank")
	}

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

	// The server enforces parallelism > 0 only when reachable. With both
	// --max-parallelism and BUILDKITE_PARALLEL_JOB_COUNT at 0, parallelism
	// resolves to 0; if the server is also unreachable the request is never
	// validated and bktec falls back to a parallelism-0 plan (nothing runs).
	// Enforce it client-side to fail fast, before any network call.
	if c.MaxParallelism == 0 && c.Parallelism <= 0 {
		c.errs.appendFieldError("parallelism", "parallelism must be greater than 0; set --max-parallelism or BUILDKITE_PARALLEL_JOB_COUNT")
	}

	if len(c.errs) > 0 {
		return c.errs
	}

	return nil
}

func (c *Config) generateOIDCToken() (token string, err error) {
	if !c.OIDC {
		return "", nil
	}

	suiteURL := fmt.Sprintf("%s/v2/analytics/organizations/%s/suites/%s", c.ServerBaseURL, c.OrganizationSlug, c.SuiteSlug)
	var tokenWriter strings.Builder
	var errorWriter strings.Builder
	lifetime := strconv.Itoa(int(c.OIDCLifetime.Seconds()))
	// Skipping a security linter check here. The issue is "G204: Subprocess launched with a potential tainted input or cmd arguments"
	// Given that running tainted input commands is bktec's raison d'etre this is acceptable.
	cmd := exec.Command(c.BuildkiteAgentCommand, "oidc", "request-token", "--audience", suiteURL, "--lifetime", lifetime) //nolint:gosec
	cmd.Stderr = &errorWriter
	cmd.Stdout = &tokenWriter
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error generating token: %s: %v", errorWriter.String(), err)
	}

	return strings.TrimSpace(tokenWriter.String()), nil
}
