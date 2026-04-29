package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/test-engine-client/internal/api"
)

const fallbackExtra = "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual."

// ANSI color codes. Disabled when NO_COLOR is set (https://no-color.org).
var (
	colorRed    = ansi("\033[31m")
	colorYellow = ansi("\033[33m")
	colorBold   = ansi("\033[1m")
	colorReset  = ansi("\033[0m")
)

func ansi(code string) string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return code
}

// printWarn prints a recoverable warning to stderr followed by an optional hint
// and the fallback notice.
func printWarn(label, message string, hints ...string) {
	fmt.Fprintf(os.Stderr, "%s⚠️ %s: %s\n", colorYellow+colorBold, label, message)
	for _, h := range hints {
		fmt.Fprintln(os.Stderr, colorYellow+h+colorReset)
	}
	fmt.Fprintf(os.Stderr, "%s%s%s\n", colorYellow, fallbackExtra, colorReset)
}

// fatal formats an unrecoverable error message with red bold styling.
// When a is an error, it is wrapped so callers can use errors.Is/As.
func fatal(label string, a any) error {
	if e, ok := a.(error); ok {
		return fmt.Errorf("%s❌ %s: %w%s", colorRed+colorBold, label, e, colorReset)
	}
	return fmt.Errorf("%s❌ %s: %v%s", colorRed+colorBold, label, a, colorReset)
}

// handleError classifies API errors and prints user-facing messages to stderr.
// Returns nil for recoverable errors (caller should fall back to non-intelligent splitting),
// or a fatal error with a formatted message for unrecoverable failures.
func handleError(err error) error {
	if errors.Is(err, api.ErrRetryTimeout) {
		printWarn("Timeout", "Test Engine API timed out")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		printWarn("Billing Error", billingError.Message)
		return nil
	}

	// 422 from the Test Engine API on plan endpoints indicates the API is
	// administratively disabled for the org (ANALYTICS_DISABLE_API feature flag).
	// Surface it as "Unavailable" since it's a temporary, server-side disable
	// rather than a client-side validation problem.
	if unprocessableError := new(api.UnprocessableEntityError); errors.As(err, &unprocessableError) {
		printWarn("Unavailable", unprocessableError.Message)
		return nil
	}

	if notFoundError := new(api.NotFoundError); errors.As(err, &notFoundError) {
		printWarn("Not Found", notFoundError.Message, "Check BUILDKITE_ORGANIZATION_SLUG and BUILDKITE_TEST_ENGINE_SUITE_SLUG are correct.")
		return nil
	}

	if authError := new(api.AuthError); errors.As(err, &authError) {
		return fatal("Authentication Failed", authError.Message)
	}

	if forbiddenError := new(api.ForbiddenError); errors.As(err, &forbiddenError) {
		return fatal("Access Denied", forbiddenError.Message)
	}

	if badRequestError := new(api.BadRequestError); errors.As(err, &badRequestError) {
		return fatal("Invalid Request", badRequestError.Message)
	}

	return fatal("Unexpected error", err)
}

func warnErrorPlan() {
	printWarn("Error Plan", "The Test Engine API failed to generate a plan.")
}
