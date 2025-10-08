package main

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetTestFilesFromFile(t *testing.T) {
	files, err := getTestFilesFromFile("testdata/test_file_discovery/list.txt")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := []string{
		"./a_spec.rb",
		"./b_spec.rb",
		"./c_spec.rb",
		"./spec/my spec.rb",
	}
	if diff := cmp.Diff(files, expected); diff != "" {
		t.Errorf("files diff (-got +want):\n%s", diff)
	}
}

func TestGetTestFilesFromFile_Dir(t *testing.T) {
	_, err := getTestFilesFromFile("testdata")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestGetTestFilesFromFile_BinaryFile(t *testing.T) {
	path := "testdata/test_file_discovery/image.png"
	_, err := getTestFilesFromFile(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	expectedError := fmt.Sprintf("%s is not a text file", path)
	if err.Error() != expectedError {
		t.Fatalf("expected error: %q, got %v", expectedError, err)
	}
}

func TestGetTestFilesFromFile_EmptyFile(t *testing.T) {
	path := "testdata/test_file_discovery/empty_list.txt"
	_, err := getTestFilesFromFile(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	expectedError := fmt.Sprintf("no test files found in %s", path)
	if err.Error() != expectedError {
		t.Fatalf("expected error: %q, got %v", expectedError, err)
	}
}
