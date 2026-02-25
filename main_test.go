package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseMetadataJSON(t *testing.T) {
	t.Run("empty value returns empty metadata", func(t *testing.T) {
		got, err := parseStringMapJSON("", metadataEnvVar)
		if err != nil {
			t.Fatalf("parseStringMapJSON() error = %v", err)
		}

		want := map[string]string{}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("parseStringMapJSON() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("valid JSON object", func(t *testing.T) {
		got, err := parseStringMapJSON(`{"foo":"bar","diff":"line1\nline2"}`, metadataEnvVar)
		if err != nil {
			t.Fatalf("parseStringMapJSON() error = %v", err)
		}

		want := map[string]string{
			"foo":  "bar",
			"diff": "line1\nline2",
		}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("parseStringMapJSON() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("non-string values fail", func(t *testing.T) {
		_, err := parseStringMapJSON(`{"foo":1}`, metadataEnvVar)
		if err == nil {
			t.Fatalf("parseStringMapJSON() error = nil, want non-nil")
		}
	})
}

func TestParseMetadataEntries(t *testing.T) {
	t.Run("parses key value pairs", func(t *testing.T) {
		got, err := parseKeyValueEntries([]string{
			"key=value",
			"git_diff=line1\nline2",
			"eq=a=b=c",
			"empty=",
		}, "metadata")
		if err != nil {
			t.Fatalf("parseKeyValueEntries() error = %v", err)
		}

		want := map[string]string{
			"key":      "value",
			"git_diff": "line1\nline2",
			"eq":       "a=b=c",
			"empty":    "",
		}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("parseKeyValueEntries() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("missing equals fails", func(t *testing.T) {
		_, err := parseKeyValueEntries([]string{"not-a-pair"}, "metadata")
		if err == nil {
			t.Fatalf("parseKeyValueEntries() error = nil, want non-nil")
		}
	})

	t.Run("empty key fails", func(t *testing.T) {
		_, err := parseKeyValueEntries([]string{"=value"}, "metadata")
		if err == nil {
			t.Fatalf("parseKeyValueEntries() error = nil, want non-nil")
		}
	})
}

func TestBuildMetadata(t *testing.T) {
	got, err := buildStringMap(
		`{"source":"env","override":"env-value"}`,
		[]string{"override=flag-value", "extra=line1\nline2"},
		metadataEnvVar,
		"metadata",
	)
	if err != nil {
		t.Fatalf("buildStringMap() error = %v", err)
	}

	want := map[string]string{
		"source":   "env",
		"override": "flag-value",
		"extra":    "line1\nline2",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("buildStringMap() diff (-got +want):\n%s", diff)
	}
}

func TestBuildSelectionParams(t *testing.T) {
	got, err := buildStringMap(
		`{"top":"25","override":"env"}`,
		[]string{"override=flag", "percent=40"},
		selectionParamsEnvVar,
		"selection parameter",
	)
	if err != nil {
		t.Fatalf("buildStringMap() error = %v", err)
	}

	want := map[string]string{
		"top":      "25",
		"override": "flag",
		"percent":  "40",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("buildStringMap() diff (-got +want):\n%s", diff)
	}
}

