package command

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/buildkite/test-engine-client/v2/internal/api"
	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/runner"
	"github.com/buildkite/test-engine-client/v2/internal/testqueue"
)

// QueuePush discovers or reads tests and pushes them into the Test Engine queue.
func QueuePush(ctx context.Context, cfg *config.Config, testFileList string, queueEntryFile string) error {
	printStartUpMessage()

	entries, err := queueEntries(cfg, testFileList, queueEntryFile)
	if err != nil {
		return err
	}

	client := testqueue.NewClient(cfg.QueueServerBaseURL, cfg.QueueAccessToken)
	queueUUID := ""
	totalInserted := 0
	queue := queueRef(cfg)

	for offset := 0; offset < len(entries); offset += cfg.QueuePushBatchSize {
		end := min(offset+cfg.QueuePushBatchSize, len(entries))
		pushedQueueUUID, inserted, err := client.PushBatch(ctx, queue, entries[offset:end])
		if err != nil {
			return err
		}
		queueUUID = pushedQueueUUID
		totalInserted += inserted
	}

	if len(entries) == 0 {
		pushedQueueUUID, inserted, err := client.PushBatch(ctx, queue, nil)
		if err != nil {
			return err
		}
		queueUUID = pushedQueueUUID
		totalInserted += inserted
	}

	if err := client.CloseQueue(ctx, queueUUID); err != nil {
		return err
	}

	fmt.Printf("+++ Buildkite Test Engine Queue: pushed %d entries (%d inserted) to %s\n", len(entries), totalInserted, queueUUID)
	return nil
}

// QueueWorker leases tests from the Test Engine queue and runs them.
func QueueWorker(ctx context.Context, cfg *config.Config) error {
	printStartUpMessage()

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	queueClient := testqueue.NewClient(cfg.QueueServerBaseURL, cfg.QueueAccessToken)
	queueUUID, _, err := queueClient.PushBatch(ctx, queueRef(cfg), nil)
	if err != nil {
		return err
	}

	apiClient := api.NewClient(api.ClientConfig{
		ServerBaseURL:    cfg.ServerBaseURL,
		UploadBaseURL:    cfg.UploadBaseURL,
		AccessToken:      cfg.AccessToken,
		OrganizationSlug: cfg.OrganizationSlug,
	})

	for {
		leaseID, leasedEntries, drained, err := queueClient.PopBatch(ctx, queueUUID, cfg.QueueBatchSize, cfg.QueueLeaseSeconds, cfg.JobID)
		if err != nil {
			return err
		}
		if len(leasedEntries) == 0 {
			if drained {
				fmt.Printf("+++ Buildkite Test Engine Queue: queue %s is drained\n", queueUUID)
				return nil
			}

			if cfg.QueuePollSeconds > 0 {
				time.Sleep(time.Duration(cfg.QueuePollSeconds) * time.Second)
			}
			continue
		}

		tests := make([]plan.TestCase, 0, len(leasedEntries))
		entryUUIDs := make([]string, 0, len(leasedEntries))
		for _, entry := range leasedEntries {
			tests = append(tests, entry.Test)
			entryUUIDs = append(entryUUIDs, entry.UUID)
		}

		var timeline []api.Timeline
		stopHeartbeat := startQueueLeaseHeartbeat(ctx, queueClient, queueUUID, leaseID, cfg.QueueLeaseSeconds)
		runResult, runErr := runTestsWithRetry(ctx, apiClient, cfg, testRunner, &tests, cfg.MaxRetries, nil, &timeline, cfg.RetryForMutedTest, cfg.FailOnNoTests)
		_ = stopHeartbeat()

		if processSignaledError := new(runner.ProcessSignaledError); errors.As(runErr, &processSignaledError) {
			logSignalAndExit(testRunner.Name(), processSignaledError.Signal)
		}

		printReport(runResult, nil, testRunner.Name())

		if exitError := new(exec.ExitError); errors.As(runErr, &exitError) {
			if exitError.ExitCode() == 1 {
				if err := completeQueueLease(ctx, queueClient, queueUUID, leaseID, entryUUIDs); err != nil {
					return err
				}
				if runResult.OnlyMutedFailures() {
					continue
				}
			}
			return fmt.Errorf("%s exited with error: %w", testRunner.Name(), runErr)
		}
		if runErr != nil {
			requeued, err := queueClient.RequeueLease(ctx, queueUUID, leaseID)
			if err != nil {
				return err
			}
			if requeued == 0 {
				return fmt.Errorf("queue lease %s was not requeued", leaseID)
			}
			return runErr
		}

		if err := completeQueueLease(ctx, queueClient, queueUUID, leaseID, entryUUIDs); err != nil {
			return err
		}
	}
}

func completeQueueLease(ctx context.Context, queueClient *testqueue.Client, queueUUID string, leaseID string, entryUUIDs []string) error {
	deleted, err := queueClient.CompleteLease(ctx, queueUUID, leaseID, entryUUIDs)
	if err != nil {
		return err
	}
	if deleted != len(entryUUIDs) {
		return fmt.Errorf("queue lease %s completed %d entries, expected %d", leaseID, deleted, len(entryUUIDs))
	}
	return nil
}

func startQueueLeaseHeartbeat(ctx context.Context, queueClient *testqueue.Client, queueUUID string, leaseID string, leaseSeconds int) func() error {
	extendSeconds := leaseSeconds
	if extendSeconds <= 0 {
		extendSeconds = 600
	}

	intervalSeconds := extendSeconds / 2
	if intervalSeconds < 1 {
		intervalSeconds = 1
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	errs := make(chan error, 1)

	go func() {
		defer close(done)

		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if _, err := queueClient.HeartbeatLease(heartbeatCtx, queueUUID, leaseID, extendSeconds); err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
			}
		}
	}()

	return func() error {
		cancel()
		<-done
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	}
}

func queueEntries(cfg *config.Config, testFileList string, queueEntryFile string) ([]testqueue.QueueEntry, error) {
	if queueEntryFile != "" {
		return readQueueEntries(queueEntryFile, cfg)
	}

	testRunner, err := runner.DetectRunner(cfg)
	if err != nil {
		return nil, fmt.Errorf("unsupported value for BUILDKITE_TEST_ENGINE_TEST_RUNNER: %w", err)
	}

	files, err := getTestFiles(testFileList, testRunner)
	if err != nil {
		return nil, err
	}

	cases := make([]plan.TestCase, 0, len(files))
	for _, file := range files {
		cases = append(cases, plan.TestCase{
			Format: plan.TestCaseFormatFile,
			Path:   file,
		})
	}

	entries := make([]testqueue.QueueEntry, 0, len(cases))
	for _, testCase := range cases {
		entries = append(entries, queueEntryForTestCase(cfg, testCase))
	}

	return entries, nil
}

func readQueueEntries(path string, cfg *config.Config) ([]testqueue.QueueEntry, error) {
	var reader io.Reader
	if path == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening queue entry file: %w", err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	entries := []testqueue.QueueEntry{}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry testqueue.QueueEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("decoding queue entry: %w", err)
		}
		if entry.UUID == "" {
			entry.UUID = deterministicEntryUUID(cfg, entry.Test)
		}
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading queue entry file: %w", err)
	}

	return entries, nil
}

func queueEntryForTestCase(cfg *config.Config, testCase plan.TestCase) testqueue.QueueEntry {
	return testqueue.QueueEntry{
		UUID:     deterministicEntryUUID(cfg, testCase),
		Test:     testCase,
		Metadata: map[string]any{},
	}
}

func deterministicEntryUUID(cfg *config.Config, testCase plan.TestCase) string {
	encodedTest, _ := json.Marshal(testCase)
	organizationID := cfg.QueueOrganizationUUID
	if organizationID == "" {
		organizationID = cfg.OrganizationSlug
	}
	suiteID := cfg.QueueSuiteUUID
	if suiteID == "" {
		suiteID = cfg.SuiteSlug
	}
	hash := sha256.Sum256([]byte(organizationID + "\x00" + suiteID + "\x00" + cfg.BuildID + "\x00" + cfg.QueueName + "\x00" + string(encodedTest)))
	bytes := hash[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(bytes)
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}

func queueRef(cfg *config.Config) testqueue.QueueRef {
	return testqueue.QueueRef{
		OrganizationUUID: cfg.QueueOrganizationUUID,
		OrganizationSlug: cfg.OrganizationSlug,
		SuiteUUID:        cfg.QueueSuiteUUID,
		SuiteSlug:        cfg.SuiteSlug,
		BuildUUID:        cfg.BuildID,
		PipelineSlug:     cfg.QueuePipelineSlug,
		StepKey:          cfg.QueueStepKey,
		QueueName:        cfg.QueueName,
	}
}
