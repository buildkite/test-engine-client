package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/urfave/cli/v3"
)

func TestPreviewSelectionEnabled(t *testing.T) {
	truthyValues := []string{"1", "true", "TRUE", "yes", "on", "t", "y"}
	for _, value := range truthyValues {
		t.Run(value, func(t *testing.T) {
			t.Setenv(previewSelectionEnvVar, value)
			if !previewSelectionEnabled() {
				t.Fatalf("previewSelectionEnabled() = false, want true for %q", value)
			}
		})
	}

	falsyValues := []string{"", "0", "false", "off", "no", "random"}
	for _, value := range falsyValues {
		t.Run(value, func(t *testing.T) {
			t.Setenv(previewSelectionEnvVar, value)
			if previewSelectionEnabled() {
				t.Fatalf("previewSelectionEnabled() = true, want false for %q", value)
			}
		})
	}
}

func TestSelectionFlagsAreGatedByPreviewEnv(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")
	if hasSelectionFlag(runCommandFlags()) {
		t.Fatalf("runCommandFlags() unexpectedly includes selection flags when preview is disabled")
	}
	if hasSelectionFlag(planCommandFlags()) {
		t.Fatalf("planCommandFlags() unexpectedly includes selection flags when preview is disabled")
	}

	t.Setenv(previewSelectionEnvVar, "true")
	if !hasSelectionFlag(runCommandFlags()) {
		t.Fatalf("runCommandFlags() missing selection flags when preview is enabled")
	}
	if !hasSelectionFlag(planCommandFlags()) {
		t.Fatalf("planCommandFlags() missing selection flags when preview is enabled")
	}
}

func TestCollectGitMetadataFlagIsGatedByPreviewEnv(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")
	if hasFlag(planCommandFlags(), "collect-git-metadata") {
		t.Fatalf("planCommandFlags() unexpectedly includes --collect-git-metadata when preview is disabled")
	}

	t.Setenv(previewSelectionEnvVar, "true")
	if !hasFlag(planCommandFlags(), "collect-git-metadata") {
		t.Fatalf("planCommandFlags() missing --collect-git-metadata when preview is enabled")
	}
}

func TestPlanCommandIncludesParallelismFlag(t *testing.T) {
	if !hasFlag(planCommandFlags(), "parallelism") {
		t.Fatalf("planCommandFlags() missing --parallelism flag; BUILDKITE_PARALLEL_JOB_COUNT will not be bound to cfg.Parallelism for `bktec plan`, breaking split-by-example slow-file detection")
	}
}

func TestApplyPlanRequestContext_ClearsCollectGitMetadataWhenPreviewDisabled(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")

	cfg.CollectGitMetadata = true
	cfg.SelectionStrategy = "percent"
	cfg.Metadata = map[string]string{"key": "val"}

	// Create a minimal command to satisfy the function signature.
	cmd := &cli.Command{}

	if err := applyPlanRequestContext(cmd); err != nil {
		t.Fatalf("applyPlanRequestContext() error = %v", err)
	}

	if cfg.CollectGitMetadata {
		t.Errorf("cfg.CollectGitMetadata = true, want false when preview is disabled")
	}
	if cfg.SelectionStrategy != "" {
		t.Errorf("cfg.SelectionStrategy = %q, want empty when preview is disabled", cfg.SelectionStrategy)
	}
	if cfg.Metadata != nil {
		t.Errorf("cfg.Metadata = %v, want nil when preview is disabled", cfg.Metadata)
	}
}

func hasSelectionFlag(flags []cli.Flag) bool {
	for _, flag := range flags {
		for _, name := range flag.Names() {
			if name == "selection-strategy" || name == "selection-param" || name == "metadata" {
				return true
			}
		}
	}

	return false
}

func hasFlag(flags []cli.Flag, name string) bool {
	for _, flag := range flags {
		for _, n := range flag.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
}

// TestRunCommandEnvVarsBindToConfig verifies that every env var wired to a run
// command flag actually lands in the cfg struct. This guards against accidental
// removal of the Destination field from a flag definition.
func TestRunCommandEnvVarsBindToConfig(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })

	t.Setenv("BUILDKITE_ORGANIZATION_SLUG", "my-org")
	t.Setenv("BUILDKITE_BUILD_ID", "build-1")
	t.Setenv("BUILDKITE_JOB_ID", "job-2")
	t.Setenv("BUILDKITE_STEP_ID", "step-3")
	t.Setenv("BUILDKITE_BRANCH", "main")
	t.Setenv("BUILDKITE_RETRY_COUNT", "2")
	t.Setenv("BUILDKITE_PARALLEL_JOB", "1")
	t.Setenv("BUILDKITE_PARALLEL_JOB_COUNT", "4")
	t.Setenv("BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN", "access-token")
	t.Setenv("BUILDKITE_ANALYTICS_TOKEN", "upload-token")
	t.Setenv("BUILDKITE_TEST_ENGINE_SUITE_SLUG", "my-suite")
	t.Setenv("BUILDKITE_TEST_ENGINE_BASE_URL", "https://example.com")
	t.Setenv("BUILDKITE_TEST_ENGINE_TAG_FILTERS", "fast")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_CMD", "go test ./...")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_FILE_PATTERN", "**/*_test.go")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_FILE_EXCLUDE_PATTERN", "vendor/**")
	t.Setenv("BUILDKITE_TEST_ENGINE_TEST_RUNNER", "gotest")
	t.Setenv("BUILDKITE_TEST_ENGINE_RESULT_PATH", "/tmp/results.json")
	t.Setenv("BUILDKITE_TEST_ENGINE_SPLIT_BY_EXAMPLE", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_FAIL_ON_NO_TESTS", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_LOCATION_PREFIX", "app/")
	t.Setenv("BUILDKITE_TEST_ENGINE_RETRY_COUNT", "3")
	t.Setenv("BUILDKITE_TEST_ENGINE_DISABLE_RETRY_FOR_MUTED_TEST", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_RETRY_CMD", "go test -run .")
	t.Setenv("BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER", "my-plan")
	t.Setenv("BUILDKITE_TEST_ENGINE_DEBUG_ENABLED", "true")
	t.Setenv("BUILDKITE_TEST_ENGINE_OIDC", "false")
	t.Setenv("BUILDKITE_TEST_ENGINE_OIDC_LIFETIME", "1h")
	t.Setenv("BUILDKITE_TEST_ENGINE_UPLOAD_TAGS", "env=production,region=us-east-1")

	cmd := &cli.Command{
		Name:  "bktec",
		Flags: []cli.Flag{debugFlag},
		Commands: []*cli.Command{
			{
				Name:                      "run",
				DisableSliceFlagSeparator: true,
				Action:                    func(ctx context.Context, cmd *cli.Command) error { return nil },
				Flags:                     runCommandFlags(),
			},
		},
	}

	if err := cmd.Run(context.Background(), []string{"bktec", "run"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"OrganizationSlug", cfg.OrganizationSlug, "my-org"},
		{"BuildID", cfg.BuildID, "build-1"},
		{"JobID", cfg.JobID, "job-2"},
		{"StepID", cfg.StepID, "step-3"},
		{"Branch", cfg.Branch, "main"},
		{"JobRetryCount", cfg.JobRetryCount, 2},
		{"NodeIndex", cfg.NodeIndex, 1},
		{"Parallelism", cfg.Parallelism, 4},
		{"AccessToken", cfg.AccessToken, "access-token"},
		{"UploadToken", cfg.UploadToken, "upload-token"},
		{"SuiteSlug", cfg.SuiteSlug, "my-suite"},
		{"ServerBaseURL", cfg.ServerBaseURL, "https://example.com"},
		{"TagFilters", cfg.TagFilters, "fast"},
		{"TestCommand", cfg.TestCommand, "go test ./..."},
		{"TestFilePattern", cfg.TestFilePattern, "**/*_test.go"},
		{"TestFileExcludePattern", cfg.TestFileExcludePattern, "vendor/**"},
		{"TestRunner", cfg.TestRunner, "gotest"},
		{"ResultPath", cfg.ResultPath, "/tmp/results.json"},
		{"SplitByExample", cfg.SplitByExample, true},
		{"FailOnNoTests", cfg.FailOnNoTests, true},
		{"LocationPrefix", cfg.LocationPrefix, "app/"},
		{"MaxRetries", cfg.MaxRetries, 3},
		// DISABLE_RETRY_FOR_MUTED_TEST=true means RetryForMutedTest should be false (flag Action inverts the bool)
		{"RetryForMutedTest", cfg.RetryForMutedTest, false},
		{"RetryCommand", cfg.RetryCommand, "go test -run ."},
		{"Identifier", cfg.Identifier, "my-plan"},
		{"DebugEnabled", cfg.DebugEnabled, true},
		{"OIDC", cfg.OIDC, false},
		{"OIDCLifetime", cfg.OIDCLifetime, time.Hour},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("cfg.%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	wantUploadTags := map[string]string{"env": "production", "region": "us-east-1"}
	if !reflect.DeepEqual(cfg.UploadTags, wantUploadTags) {
		t.Errorf("cfg.UploadTags = %v, want %v", cfg.UploadTags, wantUploadTags)
	}
}
