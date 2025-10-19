package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/urfave/cli/v3"
)

// Structure of the JSON that is output when running `bktec plan`.
type TestPlanSummary struct {
	Identifier  string `json:"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"`
	Parallelism string `json:"BUILDKITE_TEST_ENGINE_PARALLELISM"`
}

// This command creates a test plan via the API and returns the plan identifier
// and parallelism of the plan in JSON format to STDOUT.
func Plan(ctx context.Context, cmd *cli.Command) error {
	env := env.OS{}

	cfg, err := config.New(env)
	if err != nil {
		return fmt.Errorf("Invalid configuration...\n%w", err)
	}

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("Unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	var files []string

	if cmd.String("files") != "" {
		files, err = getTestFilesFromFile(cmd.String("files"))
		if err != nil {
			return err
		}
	} else {
		files, err = testRunner.GetFiles()
		if err != nil {
			return err
		}
	}

	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseUrl:    cfg.ServerBaseUrl,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	params, err := createRequestParam(ctx, cfg, files, *apiClient, testRunner)
	if err != nil {
		return err
	}

	testPlan, err := apiClient.CreateTestPlan(ctx, cfg.SuiteSlug, params)
	if err != nil {
		return fmt.Errorf("Create test plan failed: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	data := &TestPlanSummary{
		Identifier:  testPlan.Identifier,
		Parallelism: strconv.Itoa(testPlan.Parallelism),
	}
	if err = enc.Encode(data); err != nil {
		return err
	}

	return nil
}
