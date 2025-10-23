package command

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func getTestFiles(fileList string, testRunner TestRunner) ([]string, error) {
	if fileList != "" {
		return getTestFilesFromFile(fileList)
	} else {
		return testRunner.GetFiles()
	}
}

func getTestFilesFromFile(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read files from %s", path)
	}

	contentType := http.DetectContentType(content)
	if !strings.HasPrefix(contentType, "text/") {
		return nil, fmt.Errorf("%s is not a text file", path)
	}

	lines := strings.Split(string(content), "\n")
	fileNames := []string{}
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" {
			fileNames = append(fileNames, trimmedLine)
		}
	}

	if len(fileNames) == 0 {
		return nil, fmt.Errorf("no test files found in %s", path)
	}

	return fileNames, nil
}
