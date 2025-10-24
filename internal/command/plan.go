package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/buildkite/test-engine-client/internal/config"
	"github.com/buildkite/test-engine-client/internal/debug"
	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/runner"
	"github.com/urfave/cli/v3"
)

var planWriter io.Writer = os.Stdout

// Structure of the JSON that is output when running `bktec plan`.
type TestPlanSummary struct {
	Identifier string `json:"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"`

	// Parallelism is strictly an int not a string. It's represented as a string
	// here because when this struct is Marshaled to JSON it's intended to be
	// piped into buildkite-agent env set --input-format=json -, which requires
	// string keys and string values.
	Parallelism string `json:"BUILDKITE_TEST_ENGINE_PARALLELISM"`
}

// This command creates a test plan via the API and returns the plan identifier
// and parallelism of the plan in JSON format to STDOUT.
func Plan(ctx context.Context, cmd *cli.Command) error {
	env := env.OS{}

	debug.SetDebug(env.Get("BUILDKITE_TEST_ENGINE_DEBUG_ENABLED") == "true")
	debug.SetOutput(os.Stderr)

	cfg, err := config.New(env)
	if err != nil {
		return fmt.Errorf("invalid configuration...\n%w", err)
	}

	cfg.MaxParallelism = cmd.Int("max-parallelism")

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	files, err := getTestFiles(cmd.String("files"), testRunner)
	if err != nil {
		return err
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
		return fmt.Errorf("create test plan failed: %w", err)
	}

	enc := json.NewEncoder(planWriter)
	data := &TestPlanSummary{
		Identifier:  testPlan.Identifier,
		Parallelism: strconv.Itoa(testPlan.Parallelism),
	}
	if err = enc.Encode(data); err != nil {
		return err
	}

	return nil
}

// By default command.Plan writes to os.Stdout but the output can be changed here.
func SetPlanWriter(writer io.Writer) {
	planWriter = writer
}
