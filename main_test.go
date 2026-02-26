package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseMetadataEntries_Empty(t *testing.T) {
	got, err := parseKeyValueEntries([]string{}, "metadata")
	if err != nil {
		t.Fatalf("parseKeyValueEntries() error = %v", err)
	}

	want := map[string]string{}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("parseKeyValueEntries() diff (-got +want):\n%s", diff)
	}
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
