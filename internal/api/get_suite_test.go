package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSuite(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/analytics/organizations/my-org/suites/my-suite" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "22222222-2222-2222-2222-222222222222",
			"organization_id": "11111111-1111-1111-1111-111111111111"
		}`))
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{
		OrganizationSlug: "my-org",
		ServerBaseURL:    svr.URL,
	})

	got, err := client.GetSuite(t.Context(), "my-suite")
	if err != nil {
		t.Fatalf("GetSuite() error = %v", err)
	}
	if got.OrganizationID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("OrganizationID = %q", got.OrganizationID)
	}
	if got.ID != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("ID = %q", got.ID)
	}
}
