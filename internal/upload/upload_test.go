package upload

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

func TestBuildRunEnv(t *testing.T) {
	runEnv, err := RunEnvFromEnv(mapLookup(map[string]string{
		"BUILDKITE_BUILD_ID":     "thebuild",
		"BUILDKITE_BRANCH":       "trunk",
		"BUILDKITE_COMMIT":       "cafe",
		"BUILDKITE_JOB_ID":       "thejob",
		"BUILDKITE_MESSAGE":      "hello world",
		"BUILDKITE_BUILD_NUMBER": "42",
		"BUILDKITE_BUILD_URL":    "http://localhost/builds/42",
	}))
	if err != nil {
		t.Errorf("buildRunEnv(): %v", err)
	}

	want := RunEnvMap{
		"collector":  "bktec",
		"version":    version.Version,
		"CI":         "buildkite",
		"branch":     "trunk",
		"commit_sha": "cafe",
		"job_id":     "thejob",
		"key":        "thebuild",
		"message":    "hello world",
		"number":     "42",
		"url":        "http://localhost/builds/42",
	}

	if diff := cmp.Diff(want, runEnv); diff != "" {
		t.Errorf("buildRunEnv() (-want +got):\n%s", diff)
	}
}

func TestBuildRunEnv_generic(t *testing.T) {
	runEnv, err := RunEnvFromEnv(mapLookup(map[string]string{}))
	if err != nil {
		t.Errorf("buildRunEnv(): %v", err)
	}

	want := RunEnvMap{
		"collector": "bktec",
		"version":   version.Version,
		"CI":        "generic",
		"key":       "00000000-0000-0000-0000-000000000000", // placeholder
	}

	if diff := cmp.Diff(want, runEnv, cmpKeyValidUUID()); diff != "" {
		t.Errorf("buildRunEnv() (-want +got):\n%s", diff)
	}
}

func TestUpload(t *testing.T) {
	filename, xml := createTestXML(t)
	defer os.Remove(filename)

	// receive request details from the HTTP handler
	type requestInfo struct {
		Method        string
		Path          string
		Authorization string
		Data          map[string]string
	}
	var gotRequestInfo requestInfo

	// fake API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := multipartToMap(r)
		if err != nil {
			t.Errorf("parsing request: %v", err)
		}

		gotRequestInfo = requestInfo{
			Method:        r.Method,
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			Data:          data,
		}

		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"id":"theuuid","url":"http://localhost/path/theuuid"}`)
	}))
	defer srv.Close()

	// Upload!
	cfg := Config{
		UploadUrl:  srv.URL + "/path",
		SuiteToken: "hunter2",
	}
	runEnv := RunEnvMap{
		"CI":  "buildkite",
		"key": "thekey",
	}
	format := "junit"
	ctx := context.Background()
	responseData, err := Upload(ctx, cfg, runEnv, format, filename)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// verify the HTTP request details
	wantRequestInfo := requestInfo{
		Method:        "POST",
		Path:          "/path",
		Authorization: `Token token="hunter2"`,
		Data: map[string]string{
			"data":         xml,
			"format":       "junit",
			"run_env[CI]":  "buildkite",
			"run_env[key]": "thekey",
		},
	}
	if diff := cmp.Diff(wantRequestInfo, gotRequestInfo); diff != "" {
		t.Errorf("HTTP request (-want +got):\n%s", diff)
	}

	wantResponseData := map[string]string{
		"id":  "theuuid",
		"url": "http://localhost/path/theuuid",
	}
	if diff := cmp.Diff(wantResponseData, responseData); diff != "" {
		t.Errorf("HTTP response data (-want +got):\n%s", diff)
	}
}

func TestUpload_RetriesOn5xxThenSucceeds(t *testing.T) {
	filename, _ := createTestXML(t)
	defer os.Remove(filename)

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"upload_url":"http://example/uploads/abc"}`)
	}))
	defer srv.Close()

	cfg := Config{UploadUrl: srv.URL, SuiteToken: "t"}
	resp, err := Upload(context.Background(), cfg, RunEnvMap{"key": "k"}, "junit", filename)
	if err != nil {
		t.Fatalf("Upload after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if resp["upload_url"] != "http://example/uploads/abc" {
		t.Errorf("response = %v", resp)
	}
}

func TestUpload_DoesNotRetryOn4xx(t *testing.T) {
	filename, _ := createTestXML(t)
	defer os.Remove(filename)

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := Config{UploadUrl: srv.URL, SuiteToken: "t"}
	_, err := Upload(context.Background(), cfg, RunEnvMap{"key": "k"}, "junit", filename)
	if err == nil {
		t.Fatal("expected error from 401 response")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", attempts)
	}
}

func TestUpload_SetsUserAgent(t *testing.T) {
	filename, _ := createTestXML(t)
	defer os.Remove(filename)

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := Config{UploadUrl: srv.URL, SuiteToken: "t"}
	_, _ = Upload(context.Background(), cfg, RunEnvMap{"key": "k"}, "junit", filename)
	if !strings.HasPrefix(gotUA, "Buildkite Test Engine Client/") {
		t.Errorf("User-Agent = %q, want prefix %q", gotUA, "Buildkite Test Engine Client/")
	}
}

func TestUploadFile_EndToEnd(t *testing.T) {
	filename, _ := createTestXML(t)
	defer os.Remove(filename)

	var gotData map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotData, _ = multipartToMap(r)
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	cfg := Config{UploadUrl: srv.URL, SuiteToken: "tok"}
	env := mapLookup(map[string]string{
		"BUILDKITE_BUILD_ID": "build-1",
		"BUILDKITE_BRANCH":   "main",
	})
	if err := UploadFile(context.Background(), cfg, env, filename, ""); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if got, want := gotData["format"], "junit"; got != want {
		t.Errorf("format = %q, want %q", got, want)
	}
	if got, want := gotData["run_env[key]"], "build-1"; got != want {
		t.Errorf("run_env[key] = %q, want %q", got, want)
	}
}

func TestUploadFile_MissingToken(t *testing.T) {
	cfg := Config{}
	err := UploadFile(context.Background(), cfg, mapLookup(nil), "any.xml", "")
	if err == nil || !strings.Contains(err.Error(), "BUILDKITE_ANALYTICS_TOKEN") {
		t.Errorf("err = %v, want missing-token error", err)
	}
}

func TestUploadFile_FormatOverride(t *testing.T) {
	// File has no extension, but explicit --format wins.
	f, err := os.CreateTemp("", "results")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(f, `{"ok":true}`)
	f.Close()
	defer os.Remove(f.Name())

	var gotFormat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := multipartToMap(r)
		gotFormat = data["format"]
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := Config{UploadUrl: srv.URL, SuiteToken: "t"}
	if err := UploadFile(context.Background(), cfg, mapLookup(nil), f.Name(), "json"); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if gotFormat != "json" {
		t.Errorf("format = %q, want json", gotFormat)
	}
}

func TestUploadFile_FormatInferenceFailsWithoutExtension(t *testing.T) {
	f, err := os.CreateTemp("", "results")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cfg := Config{UploadUrl: "http://unused", SuiteToken: "t"}
	err = UploadFile(context.Background(), cfg, mapLookup(nil), f.Name(), "")
	if err == nil || !strings.Contains(err.Error(), "could not infer format") {
		t.Errorf("err = %v, want infer-format error", err)
	}
}

// mapLookup adapts a map to the EnvLookup function signature.
func mapLookup(m map[string]string) EnvLookup {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// cmpKeyValidUUID is an Option for cmp.Diff that validates the values of `key`
// in two maps being compared are both valid UUIDs. Note that Comparer
// functions must be symmetric; they're run as fn(a,b) and fn(b,a).
func cmpKeyValidUUID() cmp.Option {
	return cmp.FilterPath(func(path cmp.Path) bool {
		return path.Last().String() == `["key"]`
	}, cmp.Comparer(func(a, b string) bool {
		return uuid.Validate(a) == nil && uuid.Validate(b) == nil
	}))
}

func createTestXML(t *testing.T) (string, string) {
	data := `<testsuites><testsuite><testcase classname="a" name="b" /></testsuite></testsuites>`
	f, err := os.CreateTemp("", "test*.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name(), data
}

func getMultipartBoundary(contentType string) (string, error) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	if want := "multipart/form-data"; mt != want {
		return "", fmt.Errorf("Content-Type: wanted %s, got %s", want, mt)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return "", fmt.Errorf("missing multipart boundary")
	}
	return boundary, nil
}

func multipartToMap(r *http.Request) (map[string]string, error) {
	boundary, err := getMultipartBoundary(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("getMultipartBoundary: %w", err)
	}
	mr := multipart.NewReader(r.Body, boundary)
	parsed := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("multipartToMap; NextPart: %w", err)
		}
		partData, err := io.ReadAll(p)
		if err != nil {
			return nil, fmt.Errorf("multipartToMap; ReadAll: %w", err)
		}
		parsed[p.FormName()] = string(partData)
	}
	return parsed, nil
}
