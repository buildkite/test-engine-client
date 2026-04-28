package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/internal/api"
)

const fallbackExtra = "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual."

// warn prints a recoverable warning to stderr followed by the fallback notice.
func warn(label, message string) {
	fmt.Fprintf(os.Stderr, "⚠️ %s: %s\n%s\n", label, message, fallbackExtra)
}

// handleError classifies API errors and prints user-facing messages to stderr.
// Returns nil for recoverable errors (caller should fall back to non-intelligent splitting),
// or a fatal error with a formatted message for unrecoverable failures.
func handleError(err error) error {
	if errors.Is(err, api.ErrRetryTimeout) {
		warn("Timeout", "Test Engine API timed out")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		warn("Billing Error", billingError.Message)
		return nil
	}

	if unprocessableError := new(api.UnprocessableEntityError); errors.As(err, &unprocessableError) {
		warn("Unavailable", unprocessableError.Message)
		return nil
	}

	if notFoundError := new(api.NotFoundError); errors.As(err, &notFoundError) {
		fmt.Fprintf(os.Stderr, "⚠️ Not Found: %s\nCheck BUILDKITE_ORGANIZATION_SLUG and BUILDKITE_TEST_ENGINE_SUITE_SLUG are correct.\n%s\n", notFoundError.Message, fallbackExtra)
		return nil
	}

	if authError := new(api.AuthError); errors.As(err, &authError) {
		return fmt.Errorf("❌ Authentication Failed: %s", authError.Message)
	}

	if forbiddenError := new(api.ForbiddenError); errors.As(err, &forbiddenError) {
		return fmt.Errorf("❌ Access Denied: %s", forbiddenError.Message)
	}

	if badRequestError := new(api.BadRequestError); errors.As(err, &badRequestError) {
		return fmt.Errorf("❌ Invalid Request: %s", badRequestError.Message)
	}

	return err
}

func warnErrorPlan() {
	fmt.Fprintf(os.Stderr, "⚠️ Error Plan: The Test Engine API failed to generate a plan.\n%s\n", fallbackExtra)
}
