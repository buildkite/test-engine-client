package upload

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/version"
	"github.com/google/uuid"
	"golang.org/x/net/context"
)

type RunEnvMap map[string]string

// Config is upload-specific configuration, but may also contain configuration
// that is redundant with config.Config, since package upload isn't really
// unified/integrated with the rest of bktec yet.
type Config struct {
	// UploadUrl is the Test Engine upload API endpoint e.g. https://analytics-api.buildkite.com/v1/uploads
	UploadUrl string

	// SuiteToken is the Test Engine upload API suite authentication token
	SuiteToken string
}

func ConfigFromEnv(env env.Env) (Config, error) {
	url := env.Get("BUILDKITE_TEST_ENGINE_UPLOAD_URL")
	if url == "" {
		url = "https://analytics-api.buildkite.com/v1/uploads"
	}

	token := env.Get("BUILDKITE_ANALYTICS_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("BUILDKITE_ANALYTICS_TOKEN missing")
	}

	return Config{
		UploadUrl:  url,
		SuiteToken: token,
	}, nil
}

// UploadCLI is a CLI entrypoint for uploading results to Test Engine.
func UploadCLI(flag *flag.FlagSet, env env.Env) error {
	cfg, err := ConfigFromEnv(env)
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	filename := flag.Arg(1)
	if filename == "" {
		return fmt.Errorf("expected path to JUnit XML or JSON file")
	}

	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("file does not exist: %s", filename)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filename)
	}

	var format string
	switch filepath.Ext(filename) {
	case ".xml":
		format = "junit"
	case ".json":
		format = "json"
	default:
		return fmt.Errorf("could not infer format (JUnit / JSON) from filename")
	}

	runEnv, err := RunEnvFromEnv(env)
	if err != nil {
		return fmt.Errorf("unable to derive runEnv: %w", err)
	}

	slog.Info("Uploading", "key", runEnv["key"], "format", format, "filename", filename)

	ctx := context.Background()
	respData, err := Upload(ctx, cfg, runEnv, format, filename)
	if err != nil {
		return err
	}

	slog.Info("Upload successful", "url", respData["upload_url"])

	return nil
}

// Upload sends test result data to Test Engine.
func Upload(ctx context.Context, cfg Config, runEnv RunEnvMap, format string, filename string) (map[string]string, error) {
	body, err := buildUploadData(runEnv, format, filename)
	if err != nil {
		return nil, fmt.Errorf("preparing upload data: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.UploadUrl,
		body.buf,
	)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", body.writer.FormDataContentType())
	req.Header.Set("Authorization", fmt.Sprintf(`Token token="%s"`, cfg.SuiteToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP error: %w", err)
	}
	defer resp.Body.Close()

	status := resp.Status

	// Currently this should get HTTP 202 Accepted, but let's be a bit permissive to future changes.
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf(
			"expected HTTP %d or %d from Upload API, got %s",
			http.StatusCreated,
			http.StatusAccepted,
			status,
		)
	}

	// try to parse the response, but just warn if that fails
	respData := make(map[string]string)
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("failed to parse response", "status", status, "error", err)
	}

	return respData, nil
}

func RunEnvFromEnv(env env.Env) (RunEnvMap, error) {
	runEnv := RunEnvMap{
		"collector": "bktec",
		"version":   version.Version,
	}

	if _, ok := env.Lookup("BUILDKITE_BUILD_ID"); ok {
		maps.Copy(runEnv, RunEnvMap{
			"CI":         "buildkite",
			"branch":     env.Get("BUILDKITE_BRANCH"),
			"commit_sha": env.Get("BUILDKITE_COMMIT"),
			"job_id":     env.Get("BUILDKITE_JOB_ID"),
			"key":        env.Get("BUILDKITE_BUILD_ID"),
			"message":    env.Get("BUILDKITE_MESSAGE"),
			"number":     env.Get("BUILDKITE_BUILD_NUMBER"),
			"url":        env.Get("BUILDKITE_BUILD_URL"),
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
