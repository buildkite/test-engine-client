package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/buildkite/test-engine-client/internal/upload"
)

// PresignedUploadResponse is the response from the presigned upload endpoint.
type PresignedUploadResponse struct {
	URI  string                     `json:"uri"`
	Form upload.PresignedUploadForm `json:"form"`
}

// PresignUpload requests a presigned S3 upload URL for commit metadata.
//
// Endpoint: POST /v2/analytics/organizations/:org/commit-metadata-backfill/presigned-upload
// This is org-scoped (not suite-scoped) because git data applies across suites.
func (c Client) PresignUpload(ctx context.Context) (PresignedUploadResponse, error) {
	url := fmt.Sprintf(
		"%s/v2/analytics/organizations/%s/commit-metadata-backfill/presigned-upload",
		c.ServerBaseUrl, c.OrganizationSlug)

	var resp PresignedUploadResponse
	_, err := c.DoWithRetry(ctx, httpRequest{Method: http.MethodPost, URL: url}, &resp)
	if err != nil {
		return PresignedUploadResponse{}, fmt.Errorf("presigning upload: %w", err)
	}
	return resp, nil
}
