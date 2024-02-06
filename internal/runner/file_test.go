package runner

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReadJsonFile(t *testing.T) {
	var report RspecReport

	testCases := []struct {
		fileName   string
		wantResult any
		wantError  string
	}{
		{
			fileName:   "file_not_exist",
			wantResult: nil,
			wantError:  "open file_not_exist: no such file or directory"},
		// happy path -> able to read file and produce json result
		// unhappy path -> able to read file but unable to unmarshall
	}

	for _, tc := range testCases {
		gotError := readJsonFile(tc.fileName, &report)

		if gotError != nil {
			msg := fmt.Errorf("%w", gotError)

			if cmp.Equal(msg, tc.wantError) {
				t.Errorf("readJsonFile(%s) error: %s; want %s", tc.fileName, msg, tc.wantError)
			}
		}
	}
}
