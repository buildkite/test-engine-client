package command

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func getTestFiles(fileList string, testRunner runner.TestRunner) ([]string, error) {
	if fileList != "" {
		return getRowsFromFile(fileList)
	} else {
		return testRunner.GetFiles()
	}
}

// getRowsFromFile reads a text file and returns each non-empty, trimmed line as
// a row. It's used for any newline-delimited list, such as test files or selectors.
func getRowsFromFile(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read rows from %s", path)
	}

	contentType := http.DetectContentType(content)
	if !strings.HasPrefix(contentType, "text/") {
		return nil, fmt.Errorf("%s is not a text file", path)
	}

	lines := strings.Split(string(content), "\n")
	rows := []string{}
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" {
			rows = append(rows, trimmedLine)
		}
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows found in %s", path)
	}

	return rows, nil
}
