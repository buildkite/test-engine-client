package util

import (
	"testing"
	// "errors"
	"math"
	"strconv"
)

func TestFormatDuration(t *testing.T) {
	testCases := []struct {
		name string
		input int
		wantString string
		wantError error
	}{
		// {
		// 	input:			1000,
		// 	wantString: "0s",
		// 	wantError:	errors.New("sad"),
		// },
		{
			name: "formats from microsecond int to second string",
			input:			1000000,
			wantString: "1s",
			wantError:	nil,
		},
		{
			name: "truncates microseconds",
			input:			1999999,
			wantString: "1s",
			wantError:	nil,
		},
		{
			name: "formats from microsecond int to minute string",
			input:			60000000,
			wantString: "1m0s",
			wantError:	nil,
		},
		{
			name: "truncates microsecond int to minute string",
			input:			60999999,
			wantString: "1m0s",
			wantError:	nil,
		},
		{
			name: "formats from microsecond int to minute and second string",
			input:			119999999,
			wantString: "1m59s",
			wantError:	nil,
		},
		{
			name: "formats from microsecond int to minute and second string",
			input:			2819999999,
			wantString: "46m59s",
			wantError:	nil,
		},
		{
			name: "truncates values less than 1s",
			input:			999999,
			wantString: "0s",
			wantError:	nil,
		},
		{
			name: "int overflow",
			input:			int(math.Pow(2, strconv.IntSize)),
			wantString: "0s",
			wantError:	nil,
		},
		// should this test be passing? or should we be throwing an error on neg values?
		{
			name: "negative int",
			input:			-6000000,
			wantString: "-6s",
			wantError:	nil,
		},
		{
			name: "zero",
			input:			0,
			wantString: "0s",
			wantError:	nil,
		},
		// should this be -0, to be consistent with prev negative int?
		{
			name: "negative zero",
			input:			-0,
			wantString: "0s",
			wantError:	nil,
		},
		// potential other tests - max value of duration?
		// types - input is string?
	}

	for _, tc := range testCases {
		gotString, gotError := FormatDuration(tc.input)
		if gotString != tc.wantString {
			t.Errorf("FormatDuration(%d) string: %s; want %s", tc.input, gotString, tc.wantString)
		}
		if gotError != tc.wantError {
			t.Errorf("FormatDuration(%d) error: %s; want %s", tc.input, gotError, tc.wantError)
		}
	}
}
