package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/internal/api"
)

// handleError classifies API errors and prints user-facing warnings to stderr.
// Returns nil for recoverable errors (caller should fall back to non-intelligent splitting),
// or the original error for unrecoverable failures.
func handleError(err error) error {
	if errors.Is(err, api.ErrRetryTimeout) {
		fmt.Fprintln(os.Stderr, "⚠️ Test Engine API timed out after 130s. Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		fmt.Fprintln(os.Stderr, billingError.Message+"\n")
		fmt.Fprintln(os.Stderr, "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if authError := new(api.AuthError); errors.As(err, &authError) {
		fmt.Fprintln(os.Stderr, "❌ Invalid API token. Check BUILDKITE_TEST_ENGINE_API_ACCESS_TOKEN is set and valid. See https://buildkite.com/docs/apis/managing-api-tokens")
		return err
	}

	if forbiddenError := new(api.ForbiddenError); errors.As(err, &forbiddenError) {
		fmt.Fprintf(os.Stderr, "❌ Access denied: %s. Check your API token has the required scopes and organization access. See https://buildkite.com/docs/apis/managing-api-tokens\n", forbiddenError.Message)
		return err
	}

	if errors.As(err, new(*api.NotFoundError)) {
		fmt.Fprintln(os.Stderr, "⚠️ Suite not found. Check BUILDKITE_TEST_ENGINE_SUITE_SLUG is correct. Falling back to non-intelligent splitting.")
		return nil
	}

	if badRequestError := new(api.BadRequestError); errors.As(err, &badRequestError) {
		fmt.Fprintf(os.Stderr, "❌ Invalid request: %s\n", badRequestError.Message)
		return err
	}

	return err
}
