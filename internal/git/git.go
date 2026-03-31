package git

import (
	"bytes"
	"context"
	"fmt"
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
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	debug.Printf("git %s", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *ExecGitRunner) OutputWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	debug.Printf("git %s (with stdin)", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
