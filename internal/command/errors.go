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
		fmt.Fprintln(os.Stderr, "⚠️ Could not fetch or create plan from server, falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	if billingError := new(api.BillingError); errors.As(err, &billingError) {
		fmt.Fprintln(os.Stderr, billingError.Message+"\n")
		fmt.Fprintln(os.Stderr, "⚠️ Falling back to non-intelligent splitting. Your build may take longer than usual.")
		return nil
	}

	return err
}
