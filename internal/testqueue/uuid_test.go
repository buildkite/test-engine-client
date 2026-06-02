package testqueue

import "testing"

func TestNewQueueUUIDReturnsV7UUID(t *testing.T) {
	queueUUID, err := NewQueueUUID()
	if err != nil {
		t.Fatalf("NewQueueUUID() error = %v", err)
	}
	if len(queueUUID) != 36 {
		t.Fatalf("len(queueUUID) = %d, want 36", len(queueUUID))
	}
	if queueUUID[14] != '7' {
		t.Fatalf("queueUUID version = %q, want 7", queueUUID[14])
	}
	if queueUUID[8] != '-' || queueUUID[13] != '-' || queueUUID[18] != '-' || queueUUID[23] != '-' {
		t.Fatalf("queueUUID = %q, want dashed UUID", queueUUID)
	}
}
