package command

import (
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/config"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/buildkite/test-engine-client/v2/internal/testqueue"
)

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
