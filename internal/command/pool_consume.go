package command

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/api"
	"github.com/buildkite/test-engine-client/v3/internal/config"
	"github.com/buildkite/test-engine-client/v3/internal/runner"
	"github.com/buildkite/test-engine-client/v3/internal/runnerexec"
)

// jestPoolRetryCommand is pool consume's default Jest retry command. Unlike the
// bktec run default, it only runs the files of the lease's failed tests.
const jestPoolRetryCommand = "npx jest --runTestsByPath {{testExamples}} --testNamePattern '{{testNamePattern}}' --json --testLocationInResults --outputFile {{resultPath}}"

// poolCommandStopTimeout is how long a test command has to exit after SIGINT
// before it is killed, like pool exec's default runner shutdown timeout.
var poolCommandStopTimeout = 90 * time.Second

// PoolConsume works like PoolExec, but runs a new test runner command for each
// lease, and for each local retry round, instead of one persistent runner.
// Results are uploaded like bktec run: by a collector in the test command, or
// by bktec after each command when result uploads are enabled.
func PoolConsume(ctx context.Context, cfg *config.Config, files string, retries int) error {
	if cfg.TestRunner == "jest" && cfg.RetryCommand == "" {
		cfg.RetryCommand = jestPoolRetryCommand
	}
	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return err
	}
	if cfg.UploadResults && cfg.UploadToken == "" {
		fmt.Println("Buildkite Test Engine Client: Warning: upload-results is enabled but no upload token was provided. Test results will not be uploaded.")
	}
	uploads := api.NewClient(api.ClientConfig{ServerBaseURL: cfg.ServerBaseURL, UploadBaseURL: cfg.UploadBaseURL, OrganizationSlug: cfg.OrganizationSlug})
	return poolWork(ctx, cfg, files, retries, func(ctx context.Context, source *poolSource) error {
		return consumeBatches(ctx, source, func(ctx context.Context, batch runnerexec.Batch, retry bool) runnerexec.Result {
			result := runPoolCommand(ctx, testRunner, batch, retry)
			if ctx.Err() == nil {
				uploadResults(ctx, uploads, cfg, testRunner)
			}
			return result
		})
	})
}

// consumeBatches runs each batch the source offers with run until the source
// finishes. It stands in for runnerexec.Run without a persistent process, and
// like it, stops dispatching and lets the source release its lease on SIGINT
// or SIGTERM.
func consumeBatches(ctx context.Context, source *poolSource, run func(ctx context.Context, batch runnerexec.Batch, retry bool) runnerexec.Result) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	// Each batch starts its own test command, so leases can start at once.
	source.Connected()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	interrupted := make(chan error, 1)
	go func() {
		select {
		case sig := <-signals:
			// Record the signal before Terminating cancels ctx, so the loop sees it.
			interrupted <- fmt.Errorf("%w by %s", runnerexec.ErrInterrupted, sig)
			source.Terminating()
			cancel()
		case <-ctx.Done():
		}
	}()
	stopped := func() error {
		select {
		case err := <-interrupted:
			return err
		default:
			return ctx.Err()
		}
	}
	for {
		batch, reason, err := source.Next()
		switch {
		case err != nil:
			// The source's outcome reports its own error.
			return nil
		case reason != "":
			return nil
		case batch == nil:
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return stopped()
			case <-timer.C:
			}
			continue
		}
		if err := source.Dispatched(*batch); err != nil {
			return err
		}
		result := run(ctx, *batch, source.retrying())
		if ctx.Err() != nil {
			// The source already treats this batch as unresolved.
			return stopped()
		}
		source.Accepted(*batch, result)
	}
}

// runPoolCommand runs one batch with the runner's test command, or its retry
// command when the batch retries failed tests, and returns its result file as
// the batch report. Failures without a usable report are errored results.
func runPoolCommand(ctx context.Context, testRunner runner.TestRunner, batch runnerexec.Batch, retry bool) runnerexec.Result {
	format := testRunner.ResultFormat()
	_, pytest := testRunner.(runner.Pytest)
	if pytest {
		// pytest's ResultFormat is for uploads, and is empty when
		// buildkite-test-collector uploads its JSON report itself.
		format = "pytest-json"
		if testRunner.ResultFormat() == "junit" {
			format = "pytest-junit"
		}
	}
	errored := func(message string, args ...any) runnerexec.Result {
		fmt.Printf("Buildkite Test Engine Client: "+message+"\n", args...)
		return runnerexec.Result{Status: "errored", ReportFormat: format}
	}
	if retry {
		fmt.Printf("+++ Buildkite Test Engine Client: ♻️ Retrying %d failed tests\n", len(batch.Tests))
	}
	// A command that fails before writing its report must not reuse the last one.
	path := testRunner.ResultFilePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errored("Failed to remove the previous %s report: %v", testRunner.Name(), err)
	}
	cmd, err := runner.BuildCommand(testRunner, batch.Tests, retry)
	if err != nil {
		return errored("%v", err)
	}
	exitErr := runPoolProcess(ctx, cmd)
	report, err := os.ReadFile(path)
	if err != nil {
		if exitErr != nil {
			return errored("%s exited with %v without a report: %v", testRunner.Name(), exitErr, err)
		}
		return errored("%s did not write a report: %v", testRunner.Name(), err)
	}
	// Like bktec run, pytest exiting other than for failed tests, such as when
	// it's interrupted, is an error even if the report has failed tests.
	if exitCode := new(exec.ExitError); pytest && errors.As(exitErr, &exitCode) && exitCode.ExitCode() != 1 {
		return errored("%s exited with %v", testRunner.Name(), exitErr)
	}
	if exitErr != nil {
		// Like bktec run, a non-zero exit fails the batch even if every test
		// passed, such as when Jest's coverage threshold isn't met.
		parsed, err := runner.ParseNativeReport(format, report)
		if err == nil && !parsed.ErrorsOutsideTests && len(parsed.ErroredFiles) == 0 && !slices.ContainsFunc(parsed.Tests, func(test runner.ReportedTest) bool { return test.Status == runner.TestStatusFailed }) {
			return errored("%s exited with %v but reported no failed tests", testRunner.Name(), exitErr)
		}
	}
	return runnerexec.Result{Status: "completed", ReportFormat: format, Report: report}
}

// runPoolProcess runs cmd with bktec's output streams. When ctx is cancelled,
// it asks cmd and its descendants to stop, then kills them if they haven't
// exited in time.
func runPoolProcess(ctx context.Context, cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Println(cmd.String())
	fmt.Println("")
	// A wrapper such as npx can exit on a signal while its test runner keeps
	// running, so cmd runs in its own process group and stops as a group.
	startProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Remove remaining descendants even when the wrapper exits first.
	defer killProcessGroup(cmd)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return err
	case <-ctx.Done():
	}
	stopProcessGroup(cmd)
	timer := time.NewTimer(poolCommandStopTimeout)
	defer timer.Stop()
	select {
	case err := <-exited:
		return err
	case <-timer.C:
		killProcessGroup(cmd)
		return <-exited
	}
}
