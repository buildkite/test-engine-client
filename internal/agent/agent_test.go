package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPromiseFailure_sendsCorrectRequest(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotType   string
		gotBody   map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := PromiseFailure(context.Background(), server.Client(), server.URL, "secret-token", "job-uuid-123", 1, "test_failure")
	if err != nil {
		t.Fatalf("PromiseFailure returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := "/jobs/job-uuid-123/promise_failure"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "Token secret-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if want := "application/json"; gotType != want {
		t.Errorf("Content-Type = %q, want %q", gotType, want)
	}

	wantBody := map[string]any{"exit_status": float64(1), "reason": "test_failure"}
	if diff := cmp.Diff(wantBody, gotBody); diff != "" {
		t.Errorf("request body diff (-want +got):\n%s", diff)
	}
}

func TestPromiseFailure_returnsErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := PromiseFailure(context.Background(), server.Client(), server.URL, "tok", "job-1", 1, "test_failure")
	if err == nil {
		t.Fatal("expected an error on HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to mention 500", err.Error())
	}
}

func TestPromiseFailure_validatesRequiredArgs(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		accessToken string
		jobID       string
	}{
		{"blank endpoint", "", "tok", "job-1"},
		{"blank token", "http://example.com", "", "job-1"},
		{"blank job ID", "http://example.com", "tok", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := PromiseFailure(context.Background(), http.DefaultClient, tc.endpoint, tc.accessToken, tc.jobID, 1, "test_failure")
			if err == nil {
				t.Fatalf("expected an error for %s, got nil", tc.name)
			}
		})
	}
}
