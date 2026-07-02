package command

import (
	"bytes"
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
	PlanOutputPlanOut
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

	// --plan-out emits what the server returns, so it takes a distinct path
	// that does not substitute a local fallback for a server error plan.
	if outputFormat == PlanOutputPlanOut {
		return planOut(ctx, cfg, testTargets, apiClient, testRunner)
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

// planOut writes the plan as the server's test_plan endpoint would return it to
// the destination given by cfg.PlanOut ("-" for stdout, otherwise a file).
// Unlike the --json and --pipeline-upload modes it does not substitute a local
// fallback for a server error plan: the server's response (including an empty
// error plan) is passed through verbatim, so the output faithfully reflects
// what the server has. A locally-computed fallback is used only when the server
// cannot be reached at all, since there is nothing to pass through.
func planOut(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) error {
	out, closeOut, err := planOutWriter(cfg.PlanOut)
	if err != nil {
		return err
	}
	defer closeOut()

	testPlan, raw, err := requestTestPlan(ctx, cfg, testTargets, apiClient, testRunner)
	if err != nil {
		// Fatal errors (auth, forbidden, bad request) are returned; soft
		// errors (timeout, billing, disabled, not found) fall through to a
		// locally-computed fallback since the server returned nothing.
		if handledErr := handleError(err); handledErr != nil {
			return handledErr
		}
		return emitLocalFallback(out, cfg)
	}

	// The server responded. Emit its exact JSON, unmodified. An error plan
	// (`{"tasks": {}}`) is passed through verbatim so --plan-out reflects the
	// server's actual response. We warn on stderr, but not via warnErrorPlan:
	// that appends a "falling back to non-intelligent splitting" notice, which
	// is untrue here since we emit the server's plan rather than a fallback.
	if len(testPlan.Tasks) == 0 {
		fmt.Fprintln(os.Stderr, "⚠️ The Test Engine API returned an empty plan.")
	}

	debug.Printf("Test plan created. Identifier: %q, Parallelism: %d", testPlan.Identifier, testPlan.Parallelism)
	plan.PrintSplitSummary(os.Stderr, testPlan)
	if testPlan.Parallelism == 0 {
		fmt.Fprintln(os.Stderr, "⚠️ Parallelism is 0, there is nothing to run.")
	}

	return writeIndentedJSON(out, raw)
}

// planOutWriter resolves the --plan-out destination: "-" writes to planWriter
// (stdout), any other value creates (or truncates) that file. The returned
// close function closes a file destination, and is a no-op for stdout.
func planOutWriter(dest string) (io.Writer, func(), error) {
	if dest == "-" {
		return planWriter, func() {}, nil
	}

	f, err := os.Create(dest)
	if err != nil {
		return nil, nil, fmt.Errorf("opening --plan-out file: %w", err)
	}
	return f, func() { f.Close() }, nil
}

// emitLocalFallback writes a locally-computed fallback plan as JSON, used by
// --plan-out when the server cannot be reached and there is nothing to pass
// through. The fallback carries no tasks, so only the identifier and
// parallelism are emitted.
func emitLocalFallback(out io.Writer, cfg *config.Config) error {
	fmt.Fprintln(os.Stderr, "⚠️ This is a locally-computed fallback plan, not a plan from the server.")

	// A fixed-shape struct of plain fields, so marshalling cannot fail.
	encoded, _ := json.MarshalIndent(struct {
		Identifier  string                `json:"identifier"`
		Parallelism int                   `json:"parallelism"`
		Tasks       map[string]*plan.Task `json:"tasks"`
	}{
		Identifier:  cfg.Identifier,
		Parallelism: fallbackParallelism(cfg),
		Tasks:       map[string]*plan.Task{},
	}, "", "  ")

	_, err := fmt.Fprintln(out, string(encoded))
	return err
}

// writeIndentedJSON re-indents raw JSON and writes it to out, leaving the
// content (keys, values, order) untouched.
func writeIndentedJSON(out io.Writer, raw []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, buf.String()); err != nil {
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

// fallbackParallelism resolves the parallelism for a locally-computed fallback
// plan, matching how bktec run derives it: prefer the dynamic --max-parallelism,
// then fall back to the static BUILDKITE_PARALLEL_JOB_COUNT. Without this, an
// unset --max-parallelism yields parallelism 0, so the fallback runs nothing.
// May still be 0 off-agent, where BUILDKITE_PARALLEL_JOB_COUNT is unset too.
func fallbackParallelism(cfg *config.Config) int {
	if cfg.MaxParallelism > 0 {
		return cfg.MaxParallelism
	}
	return cfg.Parallelism
}

// makeFallbackPlan returns the locally-computed fallback plan used when the
// server cannot generate or return one.
func makeFallbackPlan(cfg *config.Config) plan.TestPlan {
	return plan.TestPlan{
		Identifier:  cfg.Identifier,
		Parallelism: fallbackParallelism(cfg),
		Fallback:    true,
	}
}

// requestTestPlan builds the request parameters and asks the server for a plan,
// returning the decoded plan and the raw JSON response. It applies no fallback:
// callers decide how to handle an error or an empty (error) plan.
func requestTestPlan(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) (plan.TestPlan, json.RawMessage, error) {
	params, err := createRequestParam(ctx, cfg, testTargets, *apiClient, testRunner)
	if err != nil {
		return plan.TestPlan{}, nil, err
	}

	return apiClient.CreateTestPlanRaw(ctx, cfg.SuiteSlug, params)
}

// createTestPlan requests a plan and substitutes a locally-computed fallback
// when the server errors or returns an error plan, so the caller always gets a
// plan. Used by the --json and --pipeline-upload output modes.
func createTestPlan(ctx context.Context, cfg *config.Config, testTargets []string, apiClient *api.Client, testRunner runner.TestRunner) (plan.TestPlan, error) {
	testPlan, _, err := requestTestPlan(ctx, cfg, testTargets, apiClient, testRunner)
	if err != nil {
		return makeFallbackPlan(cfg), err
	}

	// The server can return an "error" plan indicated by an empty task list (i.e. `{"tasks": {}}`).
	// In this case, we should create a fallback plan.
	if len(testPlan.Tasks) == 0 {
		warnErrorPlan()
		return makeFallbackPlan(cfg), nil
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
