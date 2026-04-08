package git

import (
	"context"
	"fmt"
	"strings"
)

// FakeGitRunner returns canned responses based on the git arguments.
// It is exported for use by tests in other packages.
type FakeGitRunner struct {
	// Responses maps a key derived from args to the output string.
	Responses map[string]string
	// StdinResponses maps a key derived from args to a function that
	// takes stdin and returns the response. Used for OutputWithStdin.
	StdinResponses map[string]func(stdin string) string
}

func (f *FakeGitRunner) key(args []string) string {
	return strings.Join(args, " ")
}

func (f *FakeGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	k := f.key(args)
	if resp, ok := f.Responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("FakeGitRunner: no response for %q", k)
}

func (f *FakeGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	k := f.key(args)
	if fn, ok := f.StdinResponses[k]; ok {
		return fn(stdin), nil
	}
	if resp, ok := f.Responses[k]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("FakeGitRunner: no response for %q (with stdin)", k)
}
