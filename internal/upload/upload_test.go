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
	"testing"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

func TestConfigFromEnv(t *testing.T) {
	cfg, err := ConfigFromEnv(env.Map{
		"BUILDKITE_ANALYTICS_TOKEN": "hunter2",
	})
	if err != nil {
		t.Errorf("ConfigFromEnv(): %v", err)
	}

	want := Config{
		UploadUrl:  "https://analytics-api.buildkite.com/v1/uploads",
		SuiteToken: "hunter2",
	}

	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Errorf("ConfigFromEnv() (-want +got)\n%s", diff)
	}
}

func TestConfigFromEnv_missingToken(t *testing.T) {
	_, err := ConfigFromEnv(env.Map{})
	if err == nil {
		t.Fatal("expected error from ConfigFromEnv with no token")
	}

	want, got := "BUILDKITE_ANALYTICS_TOKEN missing", err.Error()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ConfigFromEnv() (-want +got):\n%s", diff)
	}
}

func TestConfigFromEnv_uploadURL(t *testing.T) {
	cfg, _ := ConfigFromEnv(env.Map{
		"BUILDKITE_TEST_ENGINE_UPLOAD_URL": "http://localhost:1234/foo",
		"BUILDKITE_ANALYTICS_TOKEN":        "hello",
	})

	want := Config{
		UploadUrl:  "http://localhost:1234/foo",
		SuiteToken: "hello",
	}

	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Errorf("ConfigFromEnv (-want +got)\n%s", diff)
	}
}

func TestBuildRunEnv(t *testing.T) {
	runEnv, err := RunEnvFromEnv(env.Map{
		"BUILDKITE_BUILD_ID":     "thebuild",
		"BUILDKITE_BRANCH":       "trunk",
		"BUILDKITE_COMMIT":       "cafe",
		"BUILDKITE_JOB_ID":       "thejob",
		"BUILDKITE_MESSAGE":      "hello world",
		"BUILDKITE_BUILD_NUMBER": "42",
		"BUILDKITE_BUILD_URL":    "http://localhost/builds/42",
	})
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
	runEnv, err := RunEnvFromEnv(env.Map{})
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
	f, err := os.CreateTemp("", "test.xml")
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
