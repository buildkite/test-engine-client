package runner

import (
	"fmt"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

// ReportedTest is a native result mapped to a bktec test case. Parsers normalize
// identities to match the selectors sent to the Scheduler. Selector identifies
// the file or selector that produced the test; Location is an alternate file:line
// identity when the runnable TestCase.Path uses a framework ID.
type ReportedTest struct {
	TestCase plan.TestCase
	Status   TestStatus
	Selector string
	Location string
}

type ParsedReport struct {
	Tests []ReportedTest
	// ErrorsOutsideTests reports an error the report can't attribute to a file,
	// so it applies to every test in the batch.
	ErrorsOutsideTests bool
	// ErroredFiles lists files that failed outside their tests, for example
	// because they didn't load. Only those files' tests are affected.
	ErroredFiles []string
	// ErroredFilesStoppedRun reports that the runner ran no tests because
	// ErroredFiles failed to load, as Playwright does. When they are all files
	// of the batch, the batch's other tests can run again without them.
	ErroredFilesStoppedRun bool
	// UnselectedTests reports that the runner also runs tests outside the files
	// it was given, such as Playwright's setup projects. Those tests are ignored
	// unless one fails, which errors the batch.
	UnselectedTests bool
}

// ParseNativeReport selects the runner's parser using the reported format.
// A completed protocol envelope only means the runner produced a report.
func ParseNativeReport(format string, data []byte) (ParsedReport, error) {
	switch format {
	case "rspec-json":
		return parseRSpecBatchReport(data)
	case "jest-json":
		return parseJestBatchReport(data)
	case "playwright-json":
		return parsePlaywrightBatchReport(data)
	case "pytest-json":
		return parsePytestJSONBatchReport(data)
	case "pytest-junit":
		return parsePytestJUnitBatchReport(data)
	default:
		return ParsedReport{}, fmt.Errorf("unsupported runner report format %q", format)
	}
}
