package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/buildkite/roko"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/uuid"
)

// userAgent matches the format used by internal/api so all bktec HTTP
// traffic is identifiable in server logs.
var userAgent = fmt.Sprintf(
	"Buildkite Test Engine Client/%s (%s/%s)",
	version.Version, runtime.GOOS, runtime.GOARCH,
)

// EnvLookup mirrors the signature of os.LookupEnv so callers can pass it
// directly, while tests can substitute a map-backed lookup.
type EnvLookup func(key string) (value string, ok bool)

type RunEnvMap map[string]string

// Config is upload-specific configuration. UploadUrl and SuiteToken are
// typically populated from cli flags in cmd/main.
type Config struct {
	// UploadUrl is the Test Engine upload API endpoint e.g. https://analytics-api.buildkite.com/v1/uploads
	UploadUrl string

	// SuiteToken is the Test Engine upload API suite authentication token.
	SuiteToken string
}

// DefaultUploadUrl is used when Config.UploadUrl is empty.
const DefaultUploadUrl = "https://analytics-api.buildkite.com/v1/uploads"

// uploadTimeout caps the total time for a single upload request, including
// connection, TLS handshake, request body upload, and response read. Test
// result files are typically small, but generous headroom protects against
// slow networks and avoids the http.DefaultClient's "wait forever" default.
const uploadTimeout = 5 * time.Minute

// httpClient is the HTTP client used for upload requests.
var httpClient = &http.Client{Timeout: uploadTimeout}

// validFormats are the upload formats accepted by Test Engine.
var validFormats = map[string]bool{"junit": true, "json": true}

// inferFormat picks an upload format based on the filename extension.
func inferFormat(filename string) (string, error) {
	switch filepath.Ext(filename) {
	case ".xml":
		return "junit", nil
	case ".json":
		return "json", nil
	default:
		return "", fmt.Errorf("could not infer format from filename %q; pass --format junit|json", filename)
	}
}

func validateFormat(format string) error {
	if !validFormats[format] {
		return fmt.Errorf("invalid format %q; must be one of: junit, json", format)
	}
	return nil
}

// UploadFile uploads the given test results file to Test Engine, deriving
// run-env metadata from env. If format is empty, it is inferred from the
// filename extension.
func UploadFile(ctx context.Context, cfg Config, env EnvLookup, filename string, format string) error {
	if cfg.SuiteToken == "" {
		return fmt.Errorf("BUILDKITE_ANALYTICS_TOKEN missing")
	}
	if cfg.UploadUrl == "" {
		cfg.UploadUrl = DefaultUploadUrl
	}

	if filename == "" {
		return fmt.Errorf("expected path to JUnit XML or JSON file")
	}

	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("cannot stat %s: %w", filename, err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filename)
	}

	if format == "" {
		format, err = inferFormat(filename)
		if err != nil {
			return err
		}
	} else if err := validateFormat(format); err != nil {
		return err
	}

	runEnv, err := RunEnvFromEnv(env)
	if err != nil {
		return fmt.Errorf("unable to derive runEnv: %w", err)
	}

	slog.Info("Uploading", "key", runEnv["key"], "format", format, "filename", filename)

	respData, err := Upload(ctx, cfg, runEnv, format, filename)
	if err != nil {
		return err
	}

	slog.Info("Upload successful", "url", respData["upload_url"])

	return nil
}

// Upload sends test result data to Test Engine. Transient failures (network
// errors, 429, 5xx) are retried with exponential backoff, matching the
// behaviour of the internal/api client.
func Upload(ctx context.Context, cfg Config, runEnv RunEnvMap, format string, filename string) (map[string]string, error) {
	body, err := buildUploadData(runEnv, format, filename)
	if err != nil {
		return nil, fmt.Errorf("preparing upload data: %w", err)
	}

	// Snapshot the body bytes and content type so each retry attempt can
	// build a fresh request with a re-readable body.
	bodyBytes := body.buf.Bytes()
	contentType := body.writer.FormDataContentType()

	r := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	)

	var respData map[string]string
	err = r.DoWithContext(ctx, func(r *roko.Retrier) error {
		if r.AttemptCount() > 0 {
			debug.Printf("Retrying upload, attempt %d", r.AttemptCount())
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			cfg.UploadUrl,
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			r.Break()
			return fmt.Errorf("creating HTTP request: %w", err)
		}

		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", fmt.Sprintf(`Token token="%s"`, cfg.SuiteToken))
		req.Header.Set("User-Agent", userAgent)

		resp, err := httpClient.Do(req)
		if err != nil {
			// Network errors are retryable.
			return fmt.Errorf("HTTP error: %w", err)
		}
		defer resp.Body.Close()

		// Retryable server-side conditions.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return fmt.Errorf("server returned %s", resp.Status)
		}

		// Currently this should get HTTP 202 Accepted, but let's be a bit
		// permissive to future changes.
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
			r.Break()
			return fmt.Errorf(
				"expected HTTP %d or %d from Upload API, got %s",
				http.StatusCreated,
				http.StatusAccepted,
				resp.Status,
			)
		}

		// try to parse the response, but just warn if that fails
		respData = make(map[string]string)
		if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil && !errors.Is(err, io.EOF) {
			slog.Warn("failed to parse response", "status", resp.Status, "error", err)
		}
		return nil
	})

	return respData, err
}

func RunEnvFromEnv(env EnvLookup) (RunEnvMap, error) {
	get := func(k string) string { v, _ := env(k); return v }

	runEnv := RunEnvMap{
		"collector": "bktec",
		"version":   version.Version,
	}

	if _, ok := env("BUILDKITE_BUILD_ID"); ok {
		maps.Copy(runEnv, RunEnvMap{
			"CI":         "buildkite",
			"branch":     get("BUILDKITE_BRANCH"),
			"commit_sha": get("BUILDKITE_COMMIT"),
			"job_id":     get("BUILDKITE_JOB_ID"),
			"key":        get("BUILDKITE_BUILD_ID"),
			"message":    get("BUILDKITE_MESSAGE"),
			"number":     get("BUILDKITE_BUILD_NUMBER"),
			"url":        get("BUILDKITE_BUILD_URL"),
		})
	} else {
		key, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("UUID generation failed; broken PRNG? %w", err)
		}
		maps.Copy(runEnv, RunEnvMap{
			"CI":  "generic",
			"key": key.String(),
		})
	}
	return runEnv, nil
}

func buildUploadData(runEnv RunEnvMap, format string, filename string) (*MultipartBody, error) {
	var err error

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("opening %s for reading: %w", filename, err)
	}
	defer file.Close()

	body := NewMultipartBody()

	if err = body.WriteFormat(format); err != nil {
		return nil, err
	}

	if err = body.WriteRunEnv(runEnv); err != nil {
		return nil, err
	}

	if err = body.WriteDataFromFile(file); err != nil {
		return nil, err
	}

	if err = body.Close(); err != nil {
		return nil, err
	}

	return body, nil
}

type MultipartBody struct {
	writer multipart.Writer
	buf    *bytes.Buffer
}

func NewMultipartBody() *MultipartBody {
	buf := &bytes.Buffer{}
	return &MultipartBody{
		writer: *multipart.NewWriter(buf),
		buf:    buf,
	}
}

func (b *MultipartBody) WriteFormat(format string) error {
	return b.writer.WriteField("format", format)
}

func (b *MultipartBody) WriteRunEnv(runEnv RunEnvMap) error {
	for k, v := range runEnv {
		if err := b.writer.WriteField("run_env["+k+"]", v); err != nil {
			return err
		}
	}
	return nil
}

func (b *MultipartBody) WriteDataFromFile(file *os.File) error {
	part, err := b.writer.CreateFormFile("data", file.Name())
	if err != nil {
		return fmt.Errorf("MultipartBody: %w", err)
	}
	_, err = io.Copy(part, file)
	return err
}

func (b *MultipartBody) Close() error {
	return b.writer.Close()
}
