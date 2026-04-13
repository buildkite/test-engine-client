package api

import (
	"context"
	"fmt"
	"net/http"
)

// TokenInfo holds the response from the access-token introspection endpoint.
type TokenInfo struct {
	UUID        string   `json:"uuid"`
	Scopes      []string `json:"scopes"`
	Description string   `json:"description"`
}

// VerifyTokenScopes checks that the API token has the required scopes.
// Returns the token info on success, or an error listing missing scopes.
//
// Endpoint: GET /v2/access-token
// This endpoint requires no org/suite context -- just a valid token.
func (c Client) VerifyTokenScopes(ctx context.Context, requiredScopes []string) (*TokenInfo, error) {
	url := fmt.Sprintf("%s/v2/access-token", c.ServerBaseUrl)

	var info TokenInfo
	_, err := c.DoWithRetry(ctx, httpRequest{Method: http.MethodGet, URL: url}, &info)
	if err != nil {
		return nil, fmt.Errorf("verifying token: %w", err)
	}

	scopeSet := make(map[string]bool, len(info.Scopes))
	for _, s := range info.Scopes {
		scopeSet[s] = true
	}

	var missing []string
	for _, required := range requiredScopes {
		if !scopeSet[required] {
			missing = append(missing, required)
		}
	}

	if len(missing) > 0 {
		return &info, fmt.Errorf("token missing required scopes: %v (token has: %v)", missing, info.Scopes)
	}

	return &info, nil
}
