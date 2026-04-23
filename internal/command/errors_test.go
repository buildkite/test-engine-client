package command

import (
	"fmt"
	"testing"

	"github.com/buildkite/test-engine-client/internal/api"
	"github.com/stretchr/testify/assert"
)

func TestHandleError_RetryTimeout(t *testing.T) {
	getStderr := captureStderr(t)

	err := handleError(api.ErrRetryTimeout)

	assert.Nil(t, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "Could not fetch or create plan from server")
	assert.Contains(t, stderr, "falling back to non-intelligent splitting")
}

func TestHandleError_BillingError(t *testing.T) {
	getStderr := captureStderr(t)

	billingErr := &api.BillingError{Message: "Billing Error: please update your plan"}
	err := handleError(billingErr)

	assert.Nil(t, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "Billing Error: please update your plan")
	assert.Contains(t, stderr, "Falling back to non-intelligent splitting")
}

func TestHandleError_UnknownError(t *testing.T) {
	getStderr := captureStderr(t)

	originalErr := fmt.Errorf("something unexpected")
	err := handleError(originalErr)

	assert.Equal(t, originalErr, err)

	stderr := getStderr()
	assert.Empty(t, stderr)
}
