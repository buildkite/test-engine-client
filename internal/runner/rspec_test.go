package runner

import (
	"strings"
	"testing"

	"github.com/buildkite/test-splitter/internal/api"
	"github.com/google/go-cmp/cmp"
)

func TestReport(t *testing.T) {
	testRunner := Rspec{}
	task := api.Task{}
	var buf strings.Builder

	// Test that the report writer works when there are no report files
	err := testRunner.Report(&buf, task.Tests.Cases)

	if err != nil {
		t.Errorf("testRunner.Report expected no error, got error %v", err)
	}

	got := buf.String()
	want := "No report files found"
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("testRunner.Report output diff (-got +want):\n%s", diff)
	}
}
