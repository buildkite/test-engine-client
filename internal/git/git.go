// Package git provides abstractions for running git commands and collecting
// commit metadata, diffs, and fork-point information.
package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/buildkite/test-engine-client/internal/debug"
)

// GitRunner abstracts git command execution for testability.
type GitRunner interface {
	// Output runs a git command and returns its stdout as a string.
	Output(ctx context.Context, args ...string) (string, error)
	// OutputWithStdin runs a git command with stdin piped and returns stdout.
	OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error)
}

// ExecGitRunner runs git commands via os/exec.
type ExecGitRunner struct{}

func (r *ExecGitRunner) Output(ctx context.Context, args ...string) (string, error) {
	return r.run(ctx, nil, args)
}

func (r *ExecGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	return r.run(ctx, strings.NewReader(stdin), args)
}

func (r *ExecGitRunner) run(ctx context.Context, stdin io.Reader, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	argStr := strings.Join(args, " ")
	debug.Printf("git %s", argStr)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", argStr, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
