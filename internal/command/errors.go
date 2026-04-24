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
		fmt.Fprintln(os.Stderr, "⚠️ Test Engine API timed out. Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		fmt.Fprintln(os.Stderr, billingError.Message+"\n")
		fmt.Fprintln(os.Stderr, "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if notFoundError := new(api.NotFoundError); errors.As(err, &notFoundError) {
		fmt.Fprintf(os.Stderr, "⚠️ Not found: %s. Check BUILDKITE_TEST_ENGINE_SUITE_SLUG is correct. Falling back to non-intelligent splitting.\n", notFoundError.Message)
		return nil
	}

	if authError := new(api.AuthError); errors.As(err, &authError) {
		fmt.Fprintf(os.Stderr, "❌ Authentication failed: %s\n", authError.Message)
		return err
	}

	if forbiddenError := new(api.ForbiddenError); errors.As(err, &forbiddenError) {
		fmt.Fprintf(os.Stderr, "❌ Access denied: %s\n", forbiddenError.Message)
		return err
	}

	if badRequestError := new(api.BadRequestError); errors.As(err, &badRequestError) {
		fmt.Fprintf(os.Stderr, "❌ Invalid request: %s\n", badRequestError.Message)
		return err
	}

	return err
}

func warnErrorPlan() {
	fmt.Fprintln(os.Stderr, "⚠️ Server returned an error plan (possibly missing suite data or a server-side issue). Falling back to non-intelligent splitting. Upload test results first to enable intelligent splitting.")
}
