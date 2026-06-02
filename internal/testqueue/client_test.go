package testqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
)

func TestClientPushPopComplete(t *testing.T) {
	var sawAuthorization bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthorization = r.Header.Get("Authorization") == "Bearer queue-token"

		switch r.URL.Path {
		case "/v1/queues/push":
			var request struct {
				Queue   QueueRef     `json:"queue"`
				Entries []QueueEntry `json:"entries"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding push request: %v", err)
			}
			if request.Queue.QueueName != "rspec" {
				t.Fatalf("QueueName = %q, want rspec", request.Queue.QueueName)
			}
			if request.Queue.QueueUUID != "019e8713-0000-7000-8000-000000000020" {
				t.Fatalf("QueueUUID = %q, want explicit queue UUID", request.Queue.QueueUUID)
			}
			if len(request.Entries) != 1 {
				t.Fatalf("len(Entries) = %d, want 1", len(request.Entries))
			}
			_, _ = w.Write([]byte(`{"queue_uuid":"queue-uuid","inserted":1}`))

		case "/v1/queues/pop":
			_, _ = w.Write([]byte(`{"lease_id":"lease-uuid","entries":[{"uuid":"entry-uuid","test":{"path":"spec/example_spec.rb"},"metadata":{},"attempt":1,"lease_id":"lease-uuid","lease_expires_at":"2026-06-02T00:00:00Z"}],"drained":false}`))

		case "/v1/queues/complete":
			_, _ = w.Write([]byte(`{"deleted":1}`))

		case "/v1/queues/complete_and_push":
			var request struct {
				QueueUUID  string       `json:"queue_uuid"`
				LeaseID    string       `json:"lease_id"`
				EntryUUIDs []string     `json:"entry_uuids"`
				Entries    []QueueEntry `json:"entries"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding complete_and_push request: %v", err)
			}
			if request.QueueUUID != "queue-uuid" || request.LeaseID != "lease-uuid" || len(request.EntryUUIDs) != 1 {
				t.Fatalf("complete_and_push request = %#v, want queue/lease/entry UUIDs", request)
			}
			if len(request.Entries) != 1 || request.Entries[0].UUID != "retry-entry-uuid" {
				t.Fatalf("complete_and_push entries = %#v, want retry entry", request.Entries)
			}
			_, _ = w.Write([]byte(`{"deleted":1,"inserted":1}`))

		case "/v1/queues/requeue":
			_, _ = w.Write([]byte(`{"requeued":1}`))

		case "/v1/queues/heartbeat":
			var request struct {
				ExtendSeconds int `json:"extend_seconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decoding heartbeat request: %v", err)
			}
			if request.ExtendSeconds != 60 {
				t.Fatalf("ExtendSeconds = %d, want 60", request.ExtendSeconds)
			}
			_, _ = w.Write([]byte(`{"lease_expires_at":"2026-06-02T00:01:00Z"}`))

		case "/v1/queues/metrics":
			_, _ = w.Write([]byte(`{"queue_uuid":"queue-uuid","state":"closed","created_at":"2026-06-02T00:00:00Z","updated_at":"2026-06-02T00:02:00Z","expires_at":"2026-06-02T06:00:00Z","queued_entries":0,"leased_entries":0,"expired_leased_entries":0,"total_entries":0}`))

		case "/v1/queues/close":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "queue-token")
	queueUUID, inserted, err := client.PushBatch(context.Background(), QueueRef{
		QueueUUID: "019e8713-0000-7000-8000-000000000020",
		QueueName: "rspec",
	}, []QueueEntry{{
		UUID: "entry-uuid",
		Test: plan.TestCase{Path: "spec/example_spec.rb"},
	}})
	if err != nil {
		t.Fatalf("PushBatch() error = %v", err)
	}
	if queueUUID != "queue-uuid" || inserted != 1 {
		t.Fatalf("PushBatch() = %q, %d; want queue-uuid, 1", queueUUID, inserted)
	}

	leaseID, entries, drained, err := client.PopBatch(context.Background(), queueUUID, 1, 60, "job-uuid")
	if err != nil {
		t.Fatalf("PopBatch() error = %v", err)
	}
	if leaseID != "lease-uuid" || len(entries) != 1 {
		t.Fatalf("PopBatch() = %q, %d entries; want lease-uuid, 1", leaseID, len(entries))
	}
	if drained {
		t.Fatalf("PopBatch() drained = true, want false")
	}

	deleted, err := client.CompleteLease(context.Background(), queueUUID, leaseID, []string{entries[0].UUID})
	if err != nil {
		t.Fatalf("CompleteLease() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("CompleteLease() = %d, want 1", deleted)
	}
	deleted, retryInserted, err := client.CompleteLeaseAndPush(context.Background(), queueUUID, leaseID, []string{entries[0].UUID}, []QueueEntry{{
		UUID: "retry-entry-uuid",
		Test: plan.TestCase{Path: "spec/example_spec.rb"},
	}})
	if err != nil {
		t.Fatalf("CompleteLeaseAndPush() error = %v", err)
	}
	if deleted != 1 || retryInserted != 1 {
		t.Fatalf("CompleteLeaseAndPush() = %d, %d; want 1, 1", deleted, retryInserted)
	}
	requeued, err := client.RequeueLease(context.Background(), queueUUID, leaseID)
	if err != nil {
		t.Fatalf("RequeueLease() error = %v", err)
	}
	if requeued != 1 {
		t.Fatalf("RequeueLease() = %d, want 1", requeued)
	}
	leaseExpiresAt, err := client.HeartbeatLease(context.Background(), queueUUID, leaseID, 60)
	if err != nil {
		t.Fatalf("HeartbeatLease() error = %v", err)
	}
	if leaseExpiresAt.IsZero() {
		t.Fatalf("HeartbeatLease() returned zero expiry")
	}
	metrics, err := client.GetMetrics(context.Background(), queueUUID)
	if err != nil {
		t.Fatalf("GetMetrics() error = %v", err)
	}
	if metrics.QueueUUID != queueUUID || metrics.State != "closed" || metrics.TotalEntries != 0 {
		t.Fatalf("GetMetrics() = %#v, want closed empty queue", metrics)
	}
	if err := client.CloseQueue(context.Background(), queueUUID); err != nil {
		t.Fatalf("CloseQueue() error = %v", err)
	}
	if !sawAuthorization {
		t.Fatalf("client did not send bearer token")
	}
}

func TestClientReturnsQueueError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"queue is invalid"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	_, _, err := client.PushBatch(context.Background(), QueueRef{}, nil)
	if err == nil {
		t.Fatalf("PushBatch() error = nil, want error")
	}
}
