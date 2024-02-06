package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReadJsonFile(t *testing.T) {
	var report struct{ result string }

	testCases := []struct {
		fileName   string
		wantResult string
		wantError  error
	}{
		{
			fileName:   "file_not_exist",
			wantResult: "",
			wantError:  errors.New("open file_not_exist: no such file or directory")},
		{
			fileName:   filepath.Join("../../test", "fixtures", "report.json"),
			wantResult: "pass1",
			wantError:  nil},
		// unhappy path -> able to read file but unable to unmarshall
	}

	for _, tc := range testCases {
		gotError := readJsonFile(tc.fileName, &report)

		if gotError != nil {

			msg := fmt.Errorf("%w", gotError)
			fmt.Println(msg)
			if diff := cmp.Diff(msg, tc.wantError); diff != "" {
				fmt.Println("diff: ", diff)
				t.Errorf("readJsonFile(%s) error: %s; want %s", tc.fileName, msg, tc.wantError)
			}
		} else {
			// happy path test
			if cmp.Equal(report.result, tc.wantResult) {
				t.Errorf("readJsonFile(%s) got: %s; want %s", tc.fileName, report.result, tc.wantResult)
			}
		}
	}
}
