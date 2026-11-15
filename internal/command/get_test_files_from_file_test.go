package command

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetRowsFromFile(t *testing.T) {
	rows, err := getRowsFromFile("testdata/test_file_discovery/list.txt")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := []string{
		"./a_spec.rb",
		"./b_spec.rb",
		"./c_spec.rb",
		"./spec/my spec.rb",
	}
	if diff := cmp.Diff(rows, expected); diff != "" {
		t.Errorf("rows diff (-got +want):\n%s", diff)
	}
}

func TestGetRowsFromFile_Dir(t *testing.T) {
	_, err := getRowsFromFile("testdata")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestGetRowsFromFile_BinaryFile(t *testing.T) {
	path := "testdata/test_file_discovery/image.png"
	_, err := getRowsFromFile(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	expectedError := fmt.Sprintf("%s is not a text file", path)
	if err.Error() != expectedError {
		t.Fatalf("expected error: %q, got %v", expectedError, err)
	}
}

func TestGetRowsFromFile_EmptyFile(t *testing.T) {
	path := "testdata/test_file_discovery/empty_list.txt"
	_, err := getRowsFromFile(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	expectedError := fmt.Sprintf("no rows found in %s", path)
	if err.Error() != expectedError {
		t.Fatalf("expected error: %q, got %v", expectedError, err)
	}
}

func TestGetRowsFromFile_UTF8BOM(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		expected      []string
	}{
		{"files CRLF", "\ufeff ./a_spec.rb \r\n./spec/my spec.rb\r\n", []string{"./a_spec.rb", "./spec/my spec.rb"}},
		{"selectors LF", "\ufeff suite::first \n\nsuite::second\n", []string{"suite::first", "suite::second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "list.txt")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			rows, err := getRowsFromFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(rows, tc.expected); diff != "" {
				t.Errorf("rows diff (-got +want):\n%s", diff)
			}
		})
	}
}
