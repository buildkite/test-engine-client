package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func makeJWT(t *testing.T, claims map[string]string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling claims: %v", err)
	}
	return "fakeheader." + base64.RawURLEncoding.EncodeToString(payload) + ".fakesignature"
}

func TestDecodeOIDCClaims(t *testing.T) {
	token := makeJWT(t, map[string]string{
		"organization_id": "11111111-1111-1111-1111-111111111111",
		"pipeline_id":     "22222222-2222-2222-2222-222222222222",
		"build_id":        "33333333-3333-3333-3333-333333333333",
		"job_id":          "44444444-4444-4444-4444-444444444444",
		"sub":             "organization/buildkite/pipeline/bktec",
	})

	got, err := DecodeOIDCClaims(token)
	if err != nil {
		t.Fatalf("DecodeOIDCClaims() error = %v", err)
	}

	want := OIDCClaims{
		OrganizationID: "11111111-1111-1111-1111-111111111111",
		PipelineID:     "22222222-2222-2222-2222-222222222222",
		BuildID:        "33333333-3333-3333-3333-333333333333",
		JobID:          "44444444-4444-4444-4444-444444444444",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("DecodeOIDCClaims() diff (-got +want):\n%s", diff)
	}
}

func TestDecodeOIDCClaims_MissingClaims(t *testing.T) {
	token := makeJWT(t, map[string]string{
		"organization_id": "11111111-1111-1111-1111-111111111111",
		"build_id":        "33333333-3333-3333-3333-333333333333",
	})

	_, err := DecodeOIDCClaims(token)
	if err == nil {
		t.Fatal("DecodeOIDCClaims() error = nil, want missing claims error")
	}

	for _, want := range []string{"pipeline_id", "job_id", "--claim organization_id,pipeline_id,build_id,job_id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("DecodeOIDCClaims() error = %q, want substring %q", err.Error(), want)
		}
	}
}

func TestDecodeOIDCClaims_NotAJWT(t *testing.T) {
	_, err := DecodeOIDCClaims("not-a-jwt")
	if err == nil {
		t.Fatal("DecodeOIDCClaims() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "expected a JWT with 3 segments") {
		t.Errorf("DecodeOIDCClaims() error = %q, want segments error", err.Error())
	}
}

func TestDecodeOIDCClaims_InvalidPayload(t *testing.T) {
	_, err := DecodeOIDCClaims("header.!!!not-base64!!!.signature")
	if err == nil {
		t.Fatal("DecodeOIDCClaims() error = nil, want error")
	}

	if !strings.Contains(err.Error(), "decoding OIDC token payload") {
		t.Errorf("DecodeOIDCClaims() error = %q, want decoding error", err.Error())
	}
}
