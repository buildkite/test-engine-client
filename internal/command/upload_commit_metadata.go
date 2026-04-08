package command

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/upload"
	"github.com/buildkite/test-engine-client/internal/version"
)

// UploadCommitMetadata uploads a previously generated commit metadata tarball
// to Buildkite via presigned S3 POST. This is the upload-only counterpart to
// BackfillCommitMetadata, intended for cases where generation and upload happen
// in separate steps (e.g. air-gapped environments or split CI pipelines).
func UploadCommitMetadata(ctx context.Context, cfg *config.Config) error {
	fmt.Fprintf(os.Stderr, "+++ Buildkite Test Engine Client: bktec %s\n\n", version.Version)

	// 1. Verify file exists
	if _, err := os.Stat(cfg.UploadFile); err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// 2. Create API client
	apiClient := api.NewClient(api.ClientConfig{
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
		ServerBaseUrl:    cfg.ServerBaseUrl,
	})

	// 3. Verify token scopes
	if _, err := apiClient.VerifyTokenScopes(ctx, []string{"write_suites"}); err != nil {
		return fmt.Errorf("token scope check failed: %w", err)
	}
	debug.Println("Token scopes verified")

	// 4. Request presigned upload URL
	fmt.Fprintln(os.Stderr, "Requesting presigned upload URL...")
	presigned, err := apiClient.PresignUpload(ctx)
	if err != nil {
		return fmt.Errorf("presigning upload: %w", err)
	}

	// 5. Upload to S3
	fmt.Fprintln(os.Stderr, "Uploading to S3...")
	if err := upload.UploadToS3(ctx, cfg.UploadFile, presigned.Form); err != nil {
		return fmt.Errorf("uploading to S3: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Uploaded %s to %s\n", cfg.UploadFile, presigned.URI)
	return nil
}
