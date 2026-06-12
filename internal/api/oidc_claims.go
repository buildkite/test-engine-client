package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// OIDCClaims are the Buildkite-specific claims the Test Scheduler endpoints
// require in the OIDC token. They are requested at mint time via
// `buildkite-agent oidc request-token --claim organization_id,pipeline_id,build_id,job_id`.
type OIDCClaims struct {
	OrganizationID string `json:"organization_id"`
	PipelineID     string `json:"pipeline_id"`
	BuildID        string `json:"build_id"`
	JobID          string `json:"job_id"`
}

// DecodeOIDCClaims extracts the Buildkite claims from an OIDC JWT by decoding
// its payload segment. The token signature is NOT verified; the claims are only
// used to construct API request bodies, and the server independently verifies
// the token.
func DecodeOIDCClaims(token string) (OIDCClaims, error) {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return OIDCClaims{}, fmt.Errorf("invalid OIDC token: expected a JWT with 3 segments, got %d", len(segments))
	}

	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return OIDCClaims{}, fmt.Errorf("decoding OIDC token payload: %w", err)
	}

	var claims OIDCClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return OIDCClaims{}, fmt.Errorf("parsing OIDC token payload: %w", err)
	}

	var missing []string
	for _, claim := range []struct{ name, value string }{
		{"organization_id", claims.OrganizationID},
		{"pipeline_id", claims.PipelineID},
		{"build_id", claims.BuildID},
		{"job_id", claims.JobID},
	} {
		if claim.value == "" {
			missing = append(missing, claim.name)
		}
	}
	if len(missing) > 0 {
		return OIDCClaims{}, fmt.Errorf(
			"OIDC token is missing required claims: %s. The Test Scheduler requires a buildkite-agent that supports `oidc request-token --claim organization_id,pipeline_id,build_id,job_id`",
			strings.Join(missing, ", "),
		)
	}

	return claims, nil
}
