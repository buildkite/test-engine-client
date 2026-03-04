package command

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPrefixFilePath(t *testing.T) {
	path := "spec/models/user_spec.rb"

	cases := []struct {
		prefix   string
		expected string
	}{
		{
			prefix:   "",
			expected: "spec/models/user_spec.rb",
		},
		{
			prefix:   "my/project",
			expected: "my/project/spec/models/user_spec.rb",
		},
		{
			prefix:   "/home/user/my/project",
			expected: "/home/user/my/project/spec/models/user_spec.rb",
		},
		{
			prefix:   "./",
			expected: "./spec/models/user_spec.rb",
		},
	}

	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			got := prefixPath(path, c.prefix)
			if diff := cmp.Diff(got, c.expected); diff != "" {
				t.Errorf("prefixPath() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestTrimFilePathPrefix(t *testing.T) {
	cases := []struct {
		prefix   string
		filePath string
		expected string
	}{
		{
			prefix:   "my/project",
			filePath: "my/project/spec/models/user_spec.rb",
			expected: "spec/models/user_spec.rb",
		},
		{
			filePath: "/home/user/my/project/spec/models/user_spec.rb",
			prefix:   "/home/user/my/project",
			expected: "spec/models/user_spec.rb",
		},
		{
			filePath: "./spec/models/user_spec.rb",
			prefix:   "./",
			expected: "spec/models/user_spec.rb",
		},
	}

	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			got, err := trimFilePathPrefix(c.filePath, c.prefix)
			if err != nil {
				t.Errorf("trimFilePathPrefix() error = %v", err)
			}

			if diff := cmp.Diff(got, c.expected); diff != "" {
				t.Errorf("trimFilePathPrefix() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestTrimFilePathPrefix_Error(t *testing.T) {
	path := "spec/foo.rb"
	got, err := trimFilePathPrefix(path, "/absolute/path")
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
