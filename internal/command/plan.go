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
	PlanOutputFullJSON
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

	testTargets, err := getTestTargets(cfg, testRunner, testFileList)
	if err != nil {
		return err
	}

	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseURL:    cfg.ServerBaseURL,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	debug.Println("Creating test plan via API")

	// --full-json emits what the server returns, so it takes a distinct path
	// that does not substitute a local fallback for a server error plan.
	if outputFormat == PlanOutputFullJSON {
		return fullJSONPlan(ctx, cfg, testTargets, apiClient, testRunner)
	}

	testPlan, err := createTestPlan(ctx, cfg, testTargets, apiClient, testRunner)
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
		}{
			Identifier:  testPlan.Identifier,
			Parallelism: strconv.Itoa(testPlan.Parallelism),
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

// fullPlanOutput mirrors the server's test_plan endpoint response, so
// --full-json emits the same shape a fetch of that endpoint would. It
// deliberately omits plan.TestPlan.Fallback, which is client-internal and
// untagged (it would otherwise leak as "Fallback": <bool>).
type fullPlanOutput struct {
	Identifier        string                `json:"identifier"`
	Parallelism       int                   `json:"parallelism"`
	Experiment        string                `json:"experiment"`
	Tasks             map[string]*plan.Task `json:"tasks"`
	MutedTests        []plan.TestCase       `json:"muted_tests,omitempty"`
	SkippedTests      []plan.TestCase       `json:"skipped_tests,omitempty"`
	TimingMetadata    *plan.TimingMetadata  `json:"timing_metadata,omitempty"`
	KnownTimingsRatio *float64              `json:"known_timings_ratio,omitempty"`
}

func newFullPlanOutput(p plan.TestPlan) fullPlanOutput {
	return fullPlanOutput{
		Identifier:        p.Identifier,
		Parallelism:       p.Parallelism,
		Experiment:        p.Experiment,
		Tasks:             p.Tasks,
		MutedTests:        p.MutedTests,
		SkippedTests:      p.SkippedTests,
		TimingMetadata:    p.TimingMetadata,
		KnownTimingsRatio: p.KnownTimingsRatio,
	}
}

// fullJSONPlan emits the plan as the server's test_plan endpoint would return
// it. Unlike the --json and --pipeline-upload modes it does not substitute a
// local fallback for a server error plan: the server's response (including an
// empty error plan) is passed through verbatim, so the output faithfully
// reflects what the server has. A locally-computed fallback is used only when
// the server cannot be reached at all, since there is nothing to pass through.
func fullJSONPlan(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) error {
	testPlan, err := requestTestPlan(ctx, cfg, testTargets, apiClient, testRunner)
	if err != nil {
		// Fatal errors (auth, forbidden, bad request) are returned; soft
		// errors (timeout, billing, disabled, not found) fall through to a
		// locally-computed fallback since the server returned nothing.
		if handledErr := handleError(err); handledErr != nil {
			return handledErr
		}
		fmt.Fprintln(os.Stderr, "⚠️ This is a locally-computed fallback plan, not a plan from the server.")
		testPlan = makeFallbackPlan(cfg, testTargets)
	} else if len(testPlan.Tasks) == 0 {
		// The server returned an error plan (`{"tasks": {}}`). Pass it through
		// verbatim rather than substituting a fallback, so --full-json reflects
		// the server's actual response.
		warnErrorPlan()
	}

	if testPlan.Fallback {
		debug.Printf("Using fallback plan. Identifier: %q, Parallelism: %d", testPlan.Identifier, testPlan.Parallelism)
	} else {
		debug.Printf("Test plan created. Identifier: %q, Parallelism: %d", testPlan.Identifier, testPlan.Parallelism)
	}

	plan.PrintSplitSummary(os.Stderr, testPlan)

	if testPlan.Parallelism == 0 {
		fmt.Fprintln(os.Stderr, "⚠️ Parallelism is 0, there is nothing to run.")
	}

	encoded, err := json.MarshalIndent(newFullPlanOutput(testPlan), "", "  ")
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(planWriter, string(encoded)); err != nil {
		return err
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

// requestTestPlan requests a plan from the server and returns it verbatim,
// without substituting a local fallback. The returned plan may be an "error
// plan" with an empty task list (i.e. `{"tasks": {}}`) when the server could
// not generate one; callers that need a runnable plan should fall back
// themselves.
func requestTestPlan(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) (plan.TestPlan, error) {
	params, err := createRequestParam(ctx, cfg, testTargets, *apiClient, testRunner)
	if err != nil {
		return plan.TestPlan{}, err
	}

	return apiClient.CreateTestPlan(ctx, cfg.SuiteSlug, params)
}

// createTestPlan requests a plan and substitutes a locally-computed fallback
// when the server errors or returns an error plan, so the caller always gets a
// runnable plan. Used by the --json and --pipeline-upload output modes.
func createTestPlan(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) (plan.TestPlan, error) {
	testPlan, err := requestTestPlan(ctx, cfg, testTargets, apiClient, testRunner)
	if err != nil {
		return makeFallbackPlan(cfg, testTargets), err
	}

	// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
	// In this case, we should create a fallback plan.
	if len(testPlan.Tasks) == 0 {
		warnErrorPlan()
		return makeFallbackPlan(cfg, testTargets), nil
	}

	return testPlan, nil
}

func makeFallbackPlan(cfg *config.Config, testTargets []string) plan.TestPlan {
	parallelism := fallbackParallelism(cfg)
	fallbackPlan := plan.CreateFallbackPlan(testTargets, parallelism)
	fallbackPlan.Identifier = cfg.Identifier
	fallbackPlan.Parallelism = parallelism
	return fallbackPlan
}

// fallbackParallelism resolves the parallelism for a locally-computed fallback
// plan. It prefers the dynamic --max-parallelism, then BUILDKITE_PARALLEL_JOB_COUNT,
// and finally 1, so the fallback always has at least one node to distribute
// files across (plan.CreateFallbackPlan divides by parallelism).
func fallbackParallelism(cfg *config.Config) int {
	if cfg.MaxParallelism > 0 {
		return cfg.MaxParallelism
	}
	if cfg.Parallelism > 0 {
		return cfg.Parallelism
	}
	return 1
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
