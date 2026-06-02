package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/testqueue"
)

func TestQueueWorkerCompletesAndPushesRetryEntriesAtomically(t *testing.T) {
	tmpDir := t.TempDir()
	countPath := filepath.Join(tmpDir, "count")
	resultPath := filepath.Join(tmpDir, "result.json")
	runnerPath := filepath.Join(tmpDir, "runner.sh")
	if err := os.WriteFile(runnerPath, []byte(`#!/bin/sh
set -eu
count=0
if [ -f "`+countPath+`" ]; then
  count=$(cat "`+countPath+`")
fi
count=$((count + 1))
printf '%s' "$count" > "`+countPath+`"
if [ "$count" -eq 1 ]; then
  cat > "`+resultPath+`" <<'JSON'
[{"name":"smoke.test","scope":"smoke","identifier":"smoke.test","location":"smoke.test","file_name":"smoke.test","result":"failed","history":[{"section":"main","duration":0.1,"start_at":"2026-06-02T00:00:00Z","end_at":"2026-06-02T00:00:01Z"}]}]
JSON
  exit 1
fi
cat > "`+resultPath+`" <<'JSON'
[{"name":"smoke.test","scope":"smoke","identifier":"smoke.test","location":"smoke.test","file_name":"smoke.test","result":"passed","history":[{"section":"main","duration":0.1,"start_at":"2026-06-02T00:00:00Z","end_at":"2026-06-02T00:00:01Z"}]}]
JSON
exit 0
`), 0o755); err != nil {
		t.Fatalf("writing runner: %v", err)
	}

	queueUUID := "019e8713-0000-7000-8000-000000000020"
	originalEntryUUID := "019e8713-0000-7000-8000-000000000021"
	retryEntryUUID := ""
	popCount := 0
	completeCalls := 0
	completeAndPushCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/queues/pop":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding pop request: %v", err)
			}
			if _, ok := request["lease_owner"]; ok {
				t.Fatalf("worker pop request included lease_owner: %#v", request)
			}

			popCount++
			switch popCount {
			case 1:
				_, _ = w.Write([]byte(`{"lease_id":"019e8713-0000-7000-8000-000000000030","entries":[{"uuid":"` + originalEntryUUID + `","test":{"format":"file","path":"smoke.test"},"metadata":{},"attempt":1,"lease_id":"019e8713-0000-7000-8000-000000000030","lease_expires_at":"2026-06-02T00:00:00Z"}],"drained":false}`))
			case 2:
				if retryEntryUUID == "" {
					t.Fatalf("second pop happened before retry entry was captured")
				}
				_, _ = w.Write([]byte(`{"lease_id":"019e8713-0000-7000-8000-000000000031","entries":[{"uuid":"` + retryEntryUUID + `","test":{"format":"example","path":"smoke.test:smoke.test","scope":"smoke","name":"smoke.test","identifier":"smoke.test"},"metadata":{"queue_retry_count":1},"is_retry":true,"queue_priority":1,"attempt":1,"lease_id":"019e8713-0000-7000-8000-000000000031","lease_expires_at":"2026-06-02T00:00:00Z"}],"drained":false}`))
			default:
				_, _ = w.Write([]byte(`{"lease_id":"","entries":[],"drained":true}`))
			}
		case "/v1/queues/complete":
			completeCalls++
			var request struct {
				EntryUUIDs []string `json:"entry_uuids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding complete request: %v", err)
			}
			if completeAndPushCalls == 0 {
				t.Fatalf("worker called complete before complete_and_push")
			}
			if len(request.EntryUUIDs) != 1 || request.EntryUUIDs[0] != retryEntryUUID {
				t.Fatalf("complete entry UUIDs = %#v, want retry entry", request.EntryUUIDs)
			}
			_, _ = w.Write([]byte(`{"deleted":1}`))
		case "/v1/queues/complete_and_push":
			completeAndPushCalls++
			var request struct {
				EntryUUIDs []string               `json:"entry_uuids"`
				Entries    []testqueue.QueueEntry `json:"entries"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding complete_and_push request: %v", err)
			}
			if completeCalls != 0 {
				t.Fatalf("complete calls before complete_and_push = %d, want 0", completeCalls)
			}
			if len(request.EntryUUIDs) != 1 || request.EntryUUIDs[0] != originalEntryUUID {
				t.Fatalf("complete_and_push entry UUIDs = %#v, want original entry", request.EntryUUIDs)
			}
			if len(request.Entries) != 1 {
				t.Fatalf("len(retry entries) = %d, want 1", len(request.Entries))
			}
			retryEntryUUID = request.Entries[0].UUID
			if !request.Entries[0].IsRetry || request.Entries[0].QueuePriority != 1 {
				t.Fatalf("retry entry = %#v, want retry priority 1", request.Entries[0])
			}
			_, _ = w.Write([]byte(`{"deleted":1,"inserted":1}`))
		case "/v1/queues/heartbeat":
			_, _ = w.Write([]byte(`{"lease_expires_at":"2026-06-02T00:01:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		AccessToken:           "token",
		JobID:                 "019e8713-0000-7000-8000-000000000013",
		MaxRetries:            1,
		QueueAccessToken:      "queue-token",
		QueueBatchSize:        1,
		QueueLeaseSeconds:     60,
		QueuePollSeconds:      0,
		QueueRetryPosition:    "front",
		QueueServerBaseURL:    server.URL,
		QueueUUID:             queueUUID,
		ResultPath:            resultPath,
		TestCommand:           runnerPath + " {{testExamples}}",
		TestFilePattern:       "*.test",
		TestRunner:            "custom",
		UploadResults:         false,
		RetryForMutedTest:     true,
		OrganizationSlug:      "test-org",
		SuiteSlug:             "test-suite",
		BuildID:               "019e8713-0000-7000-8000-000000000012",
		QueueName:             "smoke-step",
		QueueOrganizationUUID: "019e8713-0000-7000-8000-000000000010",
		QueueSuiteUUID:        "019e8713-0000-7000-8000-000000000011",
	}

	if err := QueueWorker(context.Background(), cfg); err != nil {
		t.Fatalf("QueueWorker() error = %v", err)
	}
	if completeAndPushCalls != 1 {
		t.Fatalf("completeAndPushCalls = %d, want 1", completeAndPushCalls)
	}
	if completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1 for retry success", completeCalls)
	}
}

func TestQueueRetryEntries(t *testing.T) {
	cfg := &config.Config{
		BuildID:               "019e8713-0000-7000-8000-000000000001",
		MaxRetries:            2,
		QueueName:             "rspec",
		QueueOrganizationUUID: "019e8713-0000-7000-8000-000000000010",
		QueueRetryPosition:    "front",
		QueueSuiteUUID:        "019e8713-0000-7000-8000-000000000011",
	}
	failedTests := []plan.TestCase{{
		Format: plan.TestCaseFormatExample,
		Path:   "spec/example_spec.rb[1:1]",
		Scope:  "Example",
		Name:   "fails",
	}}
	leasedEntries := []testqueue.LeasedEntry{{
		Test:     plan.TestCase{Format: plan.TestCaseFormatFile, Path: "spec/example_spec.rb"},
		Metadata: map[string]any{queueRetryCountMetadataKey: float64(1)},
	}}

	entries := queueRetryEntries(cfg, failedTests, leasedEntries)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if !entries[0].IsRetry {
		t.Fatalf("entries[0].IsRetry = false, want true")
	}
	if entries[0].QueuePriority != 1 {
		t.Fatalf("entries[0].QueuePriority = %d, want 1", entries[0].QueuePriority)
	}
	if entries[0].Metadata[queueRetryCountMetadataKey] != 2 {
		t.Fatalf("retry metadata = %#v, want 2", entries[0].Metadata[queueRetryCountMetadataKey])
	}
	if entries[0].UUID == "" {
		t.Fatalf("entries[0].UUID is blank")
	}
	if entries[0].Test.Path != failedTests[0].Path {
		t.Fatalf("entries[0].Test.Path = %q, want %q", entries[0].Test.Path, failedTests[0].Path)
	}
}

func TestQueueRetryEntriesExhausted(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         1,
		QueueRetryPosition: "front",
	}
	failedTests := []plan.TestCase{{Path: "spec/example_spec.rb"}}
	leasedEntries := []testqueue.LeasedEntry{{
		Test:     plan.TestCase{Path: "spec/example_spec.rb"},
		Metadata: map[string]any{queueRetryCountMetadataKey: float64(1)},
	}}

	if entries := queueRetryEntries(cfg, failedTests, leasedEntries); len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0 after retry exhaustion", len(entries))
	}
}

func TestQueueRetryEntriesUsesMatchingLeasedEntryRetryCount(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         1,
		QueueRetryPosition: "front",
	}
	failedTests := []plan.TestCase{{Path: "spec/fresh_spec.rb[1:1]"}}
	leasedEntries := []testqueue.LeasedEntry{
		{
			Test:     plan.TestCase{Path: "spec/retry_spec.rb"},
			Metadata: map[string]any{queueRetryCountMetadataKey: float64(1)},
		},
		{
			Test:     plan.TestCase{Path: "spec/fresh_spec.rb"},
			Metadata: map[string]any{},
		},
	}

	entries := queueRetryEntries(cfg, failedTests, leasedEntries)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Metadata[queueRetryCountMetadataKey] != 1 {
		t.Fatalf("retry metadata = %#v, want 1", entries[0].Metadata[queueRetryCountMetadataKey])
	}
}

func TestQueueRetryEntriesDoesNotInheritUnmatchedRetryCount(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         1,
		QueueRetryPosition: "front",
	}
	failedTests := []plan.TestCase{{Path: "spec/fresh_spec.rb[1:1]"}}
	leasedEntries := []testqueue.LeasedEntry{{
		Test:     plan.TestCase{Path: "spec/retry_spec.rb"},
		Metadata: map[string]any{queueRetryCountMetadataKey: float64(1)},
	}}

	entries := queueRetryEntries(cfg, failedTests, leasedEntries)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Metadata[queueRetryCountMetadataKey] != 1 {
		t.Fatalf("retry metadata = %#v, want 1", entries[0].Metadata[queueRetryCountMetadataKey])
	}
}

func TestQueueRetryEntriesMatchesCucumberLineFailures(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         2,
		QueueRetryPosition: "front",
	}
	failedTests := []plan.TestCase{{Path: "./features/example.feature:12"}}
	leasedEntries := []testqueue.LeasedEntry{{
		Test:     plan.TestCase{Path: "features/example.feature"},
		Metadata: map[string]any{queueRetryCountMetadataKey: float64(1)},
	}}

	entries := queueRetryEntries(cfg, failedTests, leasedEntries)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Metadata[queueRetryCountMetadataKey] != 2 {
		t.Fatalf("retry metadata = %#v, want 2", entries[0].Metadata[queueRetryCountMetadataKey])
	}
}

func TestQueueRetryEntriesPreservesDuplicateSourceEntries(t *testing.T) {
	cfg := &config.Config{
		BuildID:            "019e8713-0000-7000-8000-000000000001",
		MaxRetries:         1,
		QueueName:          "rspec",
		QueueRetryPosition: "front",
	}
	failedTests := []plan.TestCase{{Path: "spec/example_spec.rb"}}
	leasedEntries := []testqueue.LeasedEntry{
		{UUID: "019e8713-0000-7000-8000-000000000101", Test: plan.TestCase{Path: "spec/example_spec.rb"}},
		{UUID: "019e8713-0000-7000-8000-000000000102", Test: plan.TestCase{Path: "spec/example_spec.rb"}},
	}

	entries := queueRetryEntries(cfg, failedTests, leasedEntries)

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].UUID == entries[1].UUID {
		t.Fatalf("retry UUIDs should be distinct for duplicate source entries")
	}
}

func TestQueueRetryEntriesBack(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         1,
		QueueRetryPosition: "back",
	}
	failedTests := []plan.TestCase{{Path: "spec/example_spec.rb"}}

	entries := queueRetryEntries(cfg, failedTests, nil)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].QueuePriority != -1 {
		t.Fatalf("entries[0].QueuePriority = %d, want -1", entries[0].QueuePriority)
	}
}

func TestQueueRetryEntriesInlineDisabled(t *testing.T) {
	cfg := &config.Config{
		MaxRetries:         1,
		QueueRetryPosition: "inline",
	}
	failedTests := []plan.TestCase{{Path: "spec/example_spec.rb"}}

	if entries := queueRetryEntries(cfg, failedTests, nil); len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0 for inline retries", len(entries))
	}
}
