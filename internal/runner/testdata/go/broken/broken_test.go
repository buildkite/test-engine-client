// Package broken deliberately fails to compile. It regression-tests bktec's
// handling of `go test` build failures, which gotestsum reports in JUnit XML
// as a synthetic testcase named "TestMain" with an empty classname and a
// failure message containing "[build failed]".
package broken

import (
	"testing"
)

func TestBroken(t *testing.T) {
	_ = undefinedSymbol
}
