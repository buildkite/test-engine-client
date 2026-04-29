package plan

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTestPlan_UnmarshalWithTimingMetadata(t *testing.T) {
	raw := `{
		"identifier": "abc",
		"parallelism": 2,
		"tasks": {
			"0": {
				"node_number": 0,
				"tests": [
					{"path": "a_spec.rb", "estimated_duration": 1500, "timing_sample_size": 7},
					{"path": "b_spec.rb", "estimated_duration": 1000, "timing_sample_size": 0}
				]
			}
		},
		"timing_metadata": {"median_duration": 1200, "default_duration": 1000}
	}`

	var p TestPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if p.TimingMetadata == nil {
		t.Fatal("TimingMetadata is nil, want non-nil")
	}
	if p.TimingMetadata.MedianDuration == nil || *p.TimingMetadata.MedianDuration != 1200 {
		t.Errorf("MedianDuration = %v, want 1200", p.TimingMetadata.MedianDuration)
	}
	if p.TimingMetadata.DefaultDuration != 1000 {
		t.Errorf("DefaultDuration = %v, want 1000", p.TimingMetadata.DefaultDuration)
	}
	gotTests := p.Tasks["0"].Tests
	wantTests := []TestCase{
		{Path: "a_spec.rb", EstimatedDuration: 1500, TimingSampleSize: 7},
		{Path: "b_spec.rb", EstimatedDuration: 1000, TimingSampleSize: 0},
	}
	if diff := cmp.Diff(gotTests, wantTests); diff != "" {
		t.Errorf("tests diff (-got +want):\n%s", diff)
	}
}

func TestTestPlan_UnmarshalWithNullMedianDuration(t *testing.T) {
	raw := `{"identifier":"x","parallelism":1,"tasks":{},"timing_metadata":{"median_duration":null,"default_duration":1000}}`

	var p TestPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.TimingMetadata == nil {
		t.Fatal("TimingMetadata is nil")
	}
	if p.TimingMetadata.MedianDuration != nil {
		t.Errorf("MedianDuration = %v, want nil", *p.TimingMetadata.MedianDuration)
	}
	if p.TimingMetadata.DefaultDuration != 1000 {
		t.Errorf("DefaultDuration = %v, want 1000", p.TimingMetadata.DefaultDuration)
	}
}

func TestTestPlan_UnmarshalWithoutTimingFields(t *testing.T) {
	// Older / cached / error plans omit timing_metadata and timing_sample_size entirely.
	raw := `{
		"identifier": "old",
		"parallelism": 1,
		"tasks": {
			"0": {"node_number": 0, "tests": [{"path": "a_spec.rb"}]}
		}
	}`

	var p TestPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.TimingMetadata != nil {
		t.Errorf("TimingMetadata = %+v, want nil", p.TimingMetadata)
	}
	if got := p.Tasks["0"].Tests[0].TimingSampleSize; got != 0 {
		t.Errorf("TimingSampleSize = %d, want 0", got)
	}
}

func TestTestPlan_RoundTrip(t *testing.T) {
	median := 1234.5
	original := TestPlan{
		Identifier:  "id",
		Parallelism: 1,
		Tasks: map[string]*Task{
			"0": {NodeNumber: 0, Tests: []TestCase{{Path: "a", TimingSampleSize: 3}}},
		},
		TimingMetadata: &TimingMetadata{MedianDuration: &median, DefaultDuration: 1000},
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TestPlan
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(original, got); diff != "" {
		t.Errorf("round-trip diff (-want +got):\n%s", diff)
	}
}
