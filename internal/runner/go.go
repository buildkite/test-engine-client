package runner

import "github.com/buildkite/test-splitter/internal/api"

type GoTests struct{}

func (GoTests) FindFiles() ([]string, error) {
	return nil, nil
}

func (GoTests) Run(testCases []string) error {
	return nil
}

func (GoTests) Report([]api.TestCase) {

}
