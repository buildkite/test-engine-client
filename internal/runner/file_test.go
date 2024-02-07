package runner

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

type Report struct {
	Result string `json:"result"`
}

func TestReadJsonFile_Errors(t *testing.T) {
	var report Report

	testCases := []struct {
		fileName    string
		wantErrorAs any
		wantErrorIs error
	}{
		{
			fileName:    "file_not_exist",
			wantErrorAs: new(*fs.PathError),
		},
		{
			fileName:    "file_not_exist",
			wantErrorIs: syscall.ENOENT,
		},
	}

	for _, tc := range testCases {
		gotError := readJsonFile(tc.fileName, &report)
		if tc.wantErrorAs != nil {
			if !errors.As(gotError, tc.wantErrorAs) {
				t.Errorf("readJsonFile(%q, &report) = %v, want %T", tc.fileName, gotError, tc.wantErrorAs)
			}
		}
		if tc.wantErrorIs != nil {
			if !errors.Is(gotError, tc.wantErrorIs) {
				t.Errorf("readJsonFile(%q, &report) = %v, want %v", tc.fileName, gotError, tc.wantErrorIs)
			}
		}
	}
}
