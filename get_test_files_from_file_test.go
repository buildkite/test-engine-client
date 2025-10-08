package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetTestFilesFromFile(t *testing.T) {
	files, err := getTestFilesFromFile("testdata/list.txt")
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
	_, err := getTestFilesFromFile("testdata/image.png")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "testdata/image.png is not a text file" {
		t.Fatalf("expected specific error, got %v", err)
	}
}
