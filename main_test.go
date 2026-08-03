package main

import (
	"context"
	"testing"

	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

func TestRunInvalidConfigurationError(t *testing.T) {
	cfg = config.New()
	t.Cleanup(func() { cfg = config.New() })
	t.Setenv(previewSelectionEnvVar, "")

	cfg.Identifier = "build/step"
	cfg.OrganizationSlug = "my-org"
	cfg.SuiteSlug = "my-suite"
	cfg.AccessToken = "access-token"
	cfg.UploadToken = "upload-token"
	cfg.TestRunner = "vitest"
	cfg.Parallelism = 1

	err := run(context.Background(), &cli.Command{})
	if err == nil {
		t.Fatal("run() error = nil, want invalid configuration error")
	}

	want := "bktec run: invalid configuration:\nBUILDKITE_TEST_ENGINE_RESULT_PATH must not be blank"
	if err.Error() != want {
		t.Errorf("run() error = %q, want %q", err, want)
	}
}

func TestParseMetadataEntries_Empty(t *testing.T) {
	got, err := parseKeyValueEntries([]string{}, "metadata")
	if err != nil {
		t.Fatalf("parseKeyValueEntries() error = %v", err)
	}

	want := map[string]string{}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("parseKeyValueEntries() diff (-got +want):\n%s", diff)
	}
}

func TestParseMetadataEntries(t *testing.T) {
	t.Run("parses key value pairs", func(t *testing.T) {
		got, err := parseKeyValueEntries([]string{
			"key=value",
			"git_diff=line1\nline2",
			"eq=a=b=c",
			"empty=",
		}, "metadata")
		if err != nil {
			t.Fatalf("parseKeyValueEntries() error = %v", err)
		}

		want := map[string]string{
			"key":      "value",
			"git_diff": "line1\nline2",
			"eq":       "a=b=c",
			"empty":    "",
		}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("parseKeyValueEntries() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("missing equals fails", func(t *testing.T) {
		_, err := parseKeyValueEntries([]string{"not-a-pair"}, "metadata")
		if err == nil {
			t.Fatalf("parseKeyValueEntries() error = nil, want non-nil")
		}
	})

	t.Run("empty key fails", func(t *testing.T) {
		_, err := parseKeyValueEntries([]string{"=value"}, "metadata")
		if err == nil {
			t.Fatalf("parseKeyValueEntries() error = nil, want non-nil")
		}
	})
}
