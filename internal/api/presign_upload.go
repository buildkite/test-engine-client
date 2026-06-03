package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/buildkite/test-engine-client/v2/internal/upload"
)

// PresignedUploadResponse is the response from the presigned upload endpoint.
type PresignedUploadResponse struct {
	URI  string                     `json:"uri"`
	Form upload.PresignedUploadForm `json:"form"`
}

// PresignUpload requests a presigned S3 upload URL for commit metadata.
//
// Endpoint: POST /v2/analytics/organizations/:org/suites/:suite/commit-metadata-backfill/presigned-upload
func (c Client) PresignUpload(ctx context.Context, suiteSlug string) (PresignedUploadResponse, error) {
	reqURL := fmt.Sprintf(
		"%s/v2/analytics/organizations/%s/suites/%s/commit-metadata-backfill/presigned-upload",
		c.ServerBaseURL, url.PathEscape(c.OrganizationSlug), url.PathEscape(suiteSlug),
	)

	var resp PresignedUploadResponse
	_, err := c.DoWithRetry(ctx, httpRequest{Method: http.MethodPost, URL: reqURL}, &resp)
	if err != nil {
		return PresignedUploadResponse{}, fmt.Errorf("presigning upload: %w", err)
	}
	return resp, nil
}
