package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/buildkite/test-engine-client/internal/version"
)

type PlanOutput int

const (
	PlanOutputJSON PlanOutput = iota
	PlanOutputPipelineUpload
)

var planWriter io.Writer = os.Stdout

var (
	pipelineUploadCommand = "buildkite-agent"
	pipelineUploadArgs    = []string{"pipeline", "upload"}
)

// This command creates a test plan via the API
func Plan(ctx context.Context, cfg *config.Config, testFileList string, outputFormat PlanOutput, template string) error {
	fmt.Fprintln(os.Stderr, "+++ Buildkite Test Engine Client: bktec "+version.Version+"\n")

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	files, err := getTestFiles(testFileList, testRunner)
	if err != nil {
		return err
	}

	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl:    cfg.ServerBaseUrl,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	testPlan, err := createTestPlan(ctx, cfg, files, apiClient, testRunner)
	if err != nil {
		if handledErr := handleError(err); handledErr != nil {
			return fmt.Errorf("create test plan failed: %w", err)
		}
	}

	switch outputFormat {

	case PlanOutputJSON:
		summary := struct {
			Identifier string `json:"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"`

			// Parallelism is strictly an int not a string. It's represented as a
			// string here because this JSON is primarily intended to be piped into
			// buildkite-agent env set --input-format=json -, which requires string
			// keys and string values.
			Parallelism string `json:"BUILDKITE_TEST_ENGINE_PARALLELISM"`
		}{
			Identifier:  testPlan.Identifier,
			Parallelism: strconv.Itoa(testPlan.Parallelism),
		}

		enc := json.NewEncoder(planWriter)
		if err = enc.Encode(summary); err != nil {
			return err
		}

	case PlanOutputPipelineUpload:
		cmd := makePipelineUploadCommand(template)

		env := os.Environ()
		identifierEnv := fmt.Sprintf("BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=%s", testPlan.Identifier)
		parallelismEnv := fmt.Sprintf("BUILDKITE_TEST_ENGINE_PARALLELISM=%d", testPlan.Parallelism)
		env = append(env, identifierEnv, parallelismEnv)
		cmd.Env = env

		fmt.Fprintf(planWriter, "Executing buildkite-agent pipeline upload with %s %s\n", identifierEnv, parallelismEnv)

		if err := cmd.Run(); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown plan format %v", outputFormat)
	}

	return nil
}

func makePipelineUploadCommand(template string) *exec.Cmd {
	args := append(pipelineUploadArgs, template)
	cmd := exec.Command(pipelineUploadCommand, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = planWriter
	return cmd
}

func createTestPlan(ctx context.Context, cfg *config.Config, files []string, apiClient *api.Client, testRunner runner.TestRunner) (plan.TestPlan, error) {
	fallbackPlan := plan.TestPlan{
		Identifier:  cfg.Identifier,
		Parallelism: cfg.MaxParallelism,
		Fallback:    true,
	}

	params, err := createRequestParam(ctx, cfg, files, *apiClient, testRunner)
	if err != nil {
		return fallbackPlan, err
	}

	testPlan, err := apiClient.CreateTestPlan(ctx, cfg.SuiteSlug, params)
	if err != nil {
		return fallbackPlan, err
	}

	return testPlan, nil
}

func handleError(err error) error {
	if errors.Is(err, api.ErrRetryTimeout) {
		fmt.Fprintln(os.Stderr, "⚠️ Could not fetch or create plan from server, falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		fmt.Fprintln(os.Stderr, billingError.Message+"\n")
		fmt.Fprintln(os.Stderr, "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	return err
}
