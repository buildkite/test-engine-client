package api

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortenUploadRetries overrides the package-level retry timing so retry-driven
// tests don't block for the full retry window. The original values are restored
// via t.Cleanup.
func shortenUploadRetries(t *testing.T) {
	t.Helper()
	origTimeout, origDelay := retryTimeout, initialDelay
	retryTimeout = 1 * time.Second
	initialDelay = 1 * time.Millisecond
	t.Cleanup(func() {
		retryTimeout = origTimeout
		initialDelay = origDelay
	})
}

func TestUploadTestResults(t *testing.T) {
	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	_, err = resultFile.WriteString(`{"examples":[]}`)
	require.NoError(t, err)
	resultFile.Close()

	var gotFormat, gotLocationPrefix string
	var gotToken string

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "multipart/form-data", mediaType)

		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			val, _ := io.ReadAll(part)
			switch part.FormName() {
			case "format":
				gotFormat = string(val)
			case "run_env[location_prefix]":
				gotLocationPrefix = string(val)
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{UploadBaseURL: svr.URL})
	err = client.UploadTestResults(t.Context(), "my-token", resultFile.Name(), "rspec-json", "./", nil)
	require.NoError(t, err)

	assert.Equal(t, "Token token=my-token", gotToken)
	assert.Equal(t, "rspec-json", gotFormat)
	assert.Equal(t, "./", gotLocationPrefix)
}

func TestUploadTestResults_ServerError(t *testing.T) {
	shortenUploadRetries(t)

	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	resultFile.Close()

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{UploadBaseURL: svr.URL})
	err = client.UploadTestResults(t.Context(), "my-token", resultFile.Name(), "rspec-json", "", nil)
	// 5xx is retried until the retry timeout, after which doWithRetry returns
	// ErrRetryTimeout.
	assert.ErrorIs(t, err, ErrRetryTimeout)
}

func TestUploadTestResults_RetriesThenSucceeds(t *testing.T) {
	shortenUploadRetries(t)

	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	_, err = resultFile.WriteString(`{"examples":[]}`)
	require.NoError(t, err)
	resultFile.Close()

	var attempts atomic.Int32
	var lastBodyLen atomic.Int64
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBodyLen.Store(int64(len(body)))
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{UploadBaseURL: svr.URL})
	err = client.UploadTestResults(t.Context(), "my-token", resultFile.Name(), "rspec-json", "", nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())
	// The multipart body is re-sent in full on the retry.
	assert.Greater(t, lastBodyLen.Load(), int64(0))
}

func TestUploadTestResults_NoRetryOn4xx(t *testing.T) {
	shortenUploadRetries(t)

	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	resultFile.Close()

	var attempts atomic.Int32
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{UploadBaseURL: svr.URL})
	err = client.UploadTestResults(t.Context(), "my-token", resultFile.Name(), "rspec-json", "", nil)
	assert.ErrorContains(t, err, "upload failed with status 400")
	assert.Equal(t, int32(1), attempts.Load())
}

func TestUploadTestResults_MissingFile(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer svr.Close()

	client := NewClient(ClientConfig{UploadBaseURL: svr.URL})
	err := client.UploadTestResults(t.Context(), "my-token", "/nonexistent/path/results.json", "rspec-json", "", nil)
	assert.ErrorContains(t, err, "opening result file")
}

func TestBuildTestResultsMultipartBody(t *testing.T) {
	t.Setenv("BUILDKITE_BUILD_ID", "build-123")
	t.Setenv("BUILDKITE_BRANCH", "main")
	t.Setenv("BUILDKITE_COMMIT", "abc123")

	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	_, err = resultFile.WriteString(`{"examples":[{"id":"./spec/foo_spec.rb[1:1]"}]}`)
	require.NoError(t, err)
	resultFile.Close()

	buf, contentType, err := buildTestResultsMultipartBody(resultFile.Name(), "rspec-json", "my/prefix", nil)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(contentType, "multipart/form-data"))

	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)

	fields := map[string]string{}
	mr := multipart.NewReader(buf, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		val, _ := io.ReadAll(part)
		if part.FormName() != "" {
			fields[part.FormName()] = string(val)
		}
	}

	cwd, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, "rspec-json", fields["format"])
	assert.Equal(t, "buildkite", fields["run_env[CI]"])
	assert.Equal(t, "build-123", fields["run_env[key]"])
	assert.Equal(t, "main", fields["run_env[branch]"])
	assert.Equal(t, "abc123", fields["run_env[commit_sha]"])
	assert.Equal(t, "my/prefix", fields["run_env[location_prefix]"])
	assert.Equal(t, cwd, fields["run_env[cwd]"])
}

func TestBuildTestResultsMultipartBody_WithTags(t *testing.T) {
	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	resultFile.Close()

	tags := map[string]string{"env": "production", "team": "platform"}
	buf, contentType, err := buildTestResultsMultipartBody(resultFile.Name(), "rspec-json", "", tags)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)

	fields := map[string]string{}
	mr := multipart.NewReader(buf, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		val, _ := io.ReadAll(part)
		if part.FormName() != "" {
			fields[part.FormName()] = string(val)
		}
	}

	assert.Equal(t, "production", fields["tags[env]"])
	assert.Equal(t, "platform", fields["tags[team]"])
}

func TestBuildTestResultsMultipartBody_NoCwdOutsideBuildkite(t *testing.T) {
	t.Setenv("BUILDKITE_BUILD_ID", "")

	resultFile, err := os.CreateTemp("", "results-*.json")
	require.NoError(t, err)
	defer os.Remove(resultFile.Name())
	resultFile.Close()

	buf, contentType, err := buildTestResultsMultipartBody(resultFile.Name(), "rspec-json", "", nil)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)

	fields := map[string]string{}
	mr := multipart.NewReader(buf, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		val, _ := io.ReadAll(part)
		if part.FormName() != "" {
			fields[part.FormName()] = string(val)
		}
	}

	assert.NotContains(t, fields, "run_env[cwd]")
}
