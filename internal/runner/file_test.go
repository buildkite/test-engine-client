package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type Report struct {
	Result string `json:"result"`
}

func TestReadJsonFile_Errors(t *testing.T) {
	var report Report

	testCases := []struct {
		fileName  string
		wantError error
	}{
		{
			fileName:  "file_not_exist",
			wantError: errors.New("open file_not_exist: no such file or directory")},
	}

	for _, tc := range testCases {
		gotError := readJsonFile(tc.fileName, &report)
		if gotError != nil {

			msg := fmt.Errorf("%w", gotError)
			fmt.Println(msg)
		}
	}
}

func TestReadJsonFile(t *testing.T) {
	var got Report
	fileName := filepath.Join("..", "..", "test", "fixtures", "report.json")
	want := "pass"

	err := readJsonFile(fileName, &got)
	if err != nil {
		t.Errorf("readJsonFile(%q, &got) = %v", fileName, err)
	}

	if diff := cmp.Diff(got.Result, want); diff != "" {
		t.Errorf("readJsonFile(%s) got: %s; want %s", fileName, got.Result, want)
	}
}
