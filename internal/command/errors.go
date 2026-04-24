package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/internal/api"
)

type errorSeverity int

const (
	severityWarning errorSeverity = iota
	severityFatal
)

func (s errorSeverity) icon() string {
	if s == severityFatal {
		return "❌"
	}
	return "⚠️"
}

// printError formats and prints a categorized error to stderr.
// Format: icon error_type: message. fallbackMessage
func printError(severity errorSeverity, errorType string, message string, fallbackMessage string) {
	out := fmt.Sprintf("%s %s: %s", severity.icon(), errorType, message)
	if fallbackMessage != "" {
		out += "\n" + fallbackMessage
	}
	fmt.Fprintln(os.Stderr, out)
}

const fallbackExtra = "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual."

// handleError classifies API errors and prints user-facing messages to stderr.
// Returns nil for recoverable errors (caller should fall back to non-intelligent splitting),
// or the original error for unrecoverable failures.
func handleError(err error) error {
	if errors.Is(err, api.ErrRetryTimeout) {
		printError(severityWarning, "Timeout", "Test Engine API timed out", fallbackExtra)
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		printError(severityWarning, "Billing Error", billingError.Message, fallbackExtra)
		return nil
	}

	if notFoundError := new(api.NotFoundError); errors.As(err, &notFoundError) {
		printError(severityWarning, "Not Found", notFoundError.Message, "Check BUILDKITE_TEST_ENGINE_SUITE_SLUG is correct. "+fallbackExtra)
		return nil
	}

	if authError := new(api.AuthError); errors.As(err, &authError) {
		printError(severityFatal, "Authentication Failed", authError.Message, "")
		return err
	}

	if forbiddenError := new(api.ForbiddenError); errors.As(err, &forbiddenError) {
		printError(severityFatal, "Access Denied", forbiddenError.Message, "")
		return err
	}

	if badRequestError := new(api.BadRequestError); errors.As(err, &badRequestError) {
		printError(severityFatal, "Invalid Request", badRequestError.Message, "")
		return err
	}

	return err
}

func warnErrorPlan() {
	printError(severityWarning, "Error Plan", "Server returned an error plan (possibly missing suite data or a server-side issue)", "Upload test results first to enable intelligent splitting. "+fallbackExtra)
}
