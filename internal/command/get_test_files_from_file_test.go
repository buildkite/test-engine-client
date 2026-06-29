package command

import (
	"fmt"
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
