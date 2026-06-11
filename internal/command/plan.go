package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/git"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
	"github.com/buildkite/test-engine-client/v2/internal/version"
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

	// Auto-collect git metadata when selection is active or explicitly requested
	if cfg.SelectionStrategy != "" || cfg.CollectGitMetadata {
		autoCollectGitMetadata(ctx, cfg, &git.ExecGitRunner{})
	}

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	files, err := getTestFiles(testFileList, testRunner)
	if err != nil {
		return err
	}

	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseURL:    cfg.ServerBaseURL,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	debug.Println("Creating test plan via API")

	testPlan, err := createTestPlan(ctx, cfg, files, apiClient, testRunner)
	if err != nil {
		if handledErr := handleError(err); handledErr != nil {
			return handledErr
		}
	}

	if testPlan.Fallback {
		debug.Printf("Using fallback plan. Identifier: %q, Parallelism: %d", testPlan.Identifier, testPlan.Parallelism)
	} else {
		debug.Printf("Test plan created. Identifier: %q, Parallelism: %d", testPlan.Identifier, testPlan.Parallelism)
	}

	plan.PrintSplitSummary(os.Stderr, testPlan)

	var schedulerPlan *api.SchedulerPlanResponse
	if cfg.SchedulerPlan {
		debug.Println("Creating Test Scheduler pool via API")
		createdSchedulerPlan, err := createSchedulerPlan(ctx, cfg, apiClient, testRunner.Name(), testPlan.Identifier)
		if err != nil {
			return err
		}
		schedulerPlan = &createdSchedulerPlan
		debug.Printf("Test Scheduler pool created. Name: %q, UUID: %q, Uploaded groups: %d", schedulerPlan.Run.QueueName, schedulerPlan.Run.UUID, schedulerPlan.UploadedGroupsCount)
	}

	switch outputFormat {

	case PlanOutputJSON:
		if testPlan.Parallelism == 0 {
			fmt.Fprintln(os.Stderr, "⚠️ Parallelism is 0, there is nothing to run.")
		}

		summary := struct {
			Identifier string `json:"BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER"`

			// Parallelism is strictly an int not a string. It's represented as a
			// string here because this JSON is primarily intended to be piped into
			// buildkite-agent env set --input-format=json -, which requires string
			// keys and string values.
			Parallelism string `json:"BUILDKITE_TEST_ENGINE_PARALLELISM"`
			PoolName    string `json:"BUILDKITE_TEST_ENGINE_POOL_NAME,omitempty"`
			PoolID      string `json:"BUILDKITE_TEST_ENGINE_POOL_ID,omitempty"`
		}{
			Identifier:  testPlan.Identifier,
			Parallelism: strconv.Itoa(testPlan.Parallelism),
		}
		if schedulerPlan != nil {
			summary.PoolName = schedulerPlan.Run.QueueName
			summary.PoolID = schedulerPlan.Run.UUID
		}

		enc := json.NewEncoder(planWriter)
		if err = enc.Encode(summary); err != nil {
			return err
		}

	case PlanOutputPipelineUpload:
		if testPlan.Parallelism == 0 {
			fmt.Fprintln(os.Stderr, "⚠️ Parallelism is 0, there is nothing to run.")
			return nil
		}

		cmd := makePipelineUploadCommand(template)

		env := os.Environ()
		identifierEnv := fmt.Sprintf("BUILDKITE_TEST_ENGINE_PLAN_IDENTIFIER=%s", testPlan.Identifier)
		parallelismEnv := fmt.Sprintf("BUILDKITE_TEST_ENGINE_PARALLELISM=%d", testPlan.Parallelism)
		env = append(env, identifierEnv, parallelismEnv)
		if schedulerPlan != nil {
			env = append(env,
				fmt.Sprintf("BUILDKITE_TEST_ENGINE_POOL_NAME=%s", schedulerPlan.Run.QueueName),
				fmt.Sprintf("BUILDKITE_TEST_ENGINE_POOL_ID=%s", schedulerPlan.Run.UUID),
			)
		}
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

func createSchedulerPlan(ctx context.Context, cfg *config.Config, apiClient *api.Client, runnerName, testPlanIdentifier string) (api.SchedulerPlanResponse, error) {
	suite, err := apiClient.FetchSuite(ctx, cfg.SuiteSlug)
	if err != nil {
		return api.SchedulerPlanResponse{}, fmt.Errorf("fetching suite for Test Scheduler plan: %w", err)
	}

	return apiClient.CreateSchedulerPlan(ctx, api.SchedulerPlanParams{
		OrganizationUUID:   cfg.OrganizationID,
		SuiteUUID:          suite.ID,
		PipelineUUID:       cfg.PipelineID,
		BuildUUID:          cfg.BuildID,
		QueueName:          cfg.SchedulerPoolName,
		Ecosystem:          runnerName,
		Framework:          runnerName,
		TestPlanIdentifier: testPlanIdentifier,
	})
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

	// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
	// In this case, we should create a fallback plan.
	if len(testPlan.Tasks) == 0 {
		warnErrorPlan()
		return fallbackPlan, nil
	}

	return testPlan, nil
}

// autoCollectGitMetadata collects git commit metadata and merges it into
// cfg.Metadata. User-provided metadata values (from --metadata) take
// precedence over auto-collected values.
func autoCollectGitMetadata(ctx context.Context, cfg *config.Config, runner git.GitRunner) {
	// Check if we're in a git repo
	if _, err := runner.Output(ctx, "rev-parse", "--git-dir"); err != nil {
		fmt.Fprintln(os.Stderr, "⚠️ Not a git repository, skipping metadata auto-collection.")
		return
	}

	// Use user-provided base_branch from --metadata if present
	explicit := cfg.Metadata["base_branch"]
	remote := cfg.Remote
	baseBranch, err := git.ResolveBaseBranch(ctx, runner, explicit, remote)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠️ Could not resolve base branch for diff metadata. "+
			"Set --metadata base_branch=<branch> if your repo uses a non-standard default branch.")
	} else {
		debug.Printf("auto-detected base branch: %s", baseBranch)
	}

	autoMetadata := git.CollectPlanMetadata(ctx, runner, baseBranch)
	cfg.Metadata = git.MergeMetadata(cfg.Metadata, autoMetadata)
}
