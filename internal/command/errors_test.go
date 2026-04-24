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
	assert.Contains(t, stderr, "⚠️ Timeout:")
	assert.Contains(t, stderr, "Test Engine API timed out")
	assert.Contains(t, stderr, "Falling back to non-intelligent splitting")
}

func TestHandleError_BillingError(t *testing.T) {
	getStderr := captureStderr(t)

	billingErr := &api.BillingError{Message: "Billing Error: please update your plan"}
	err := handleError(billingErr)

	assert.Nil(t, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "⚠️ Billing Error:")
	assert.Contains(t, stderr, "Billing Error: please update your plan")
	assert.Contains(t, stderr, "Falling back to non-intelligent splitting")
}

func TestHandleError_AuthError(t *testing.T) {
	getStderr := captureStderr(t)

	authErr := &api.AuthError{Message: "Authentication required. Please supply a valid API Access Token: https://buildkite.com/docs/apis/rest-api#authentication"}
	err := handleError(authErr)

	assert.Equal(t, authErr, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "❌ Authentication Failed:")
	assert.Contains(t, stderr, "Authentication required. Please supply a valid API Access Token")
}

func TestHandleError_ForbiddenError(t *testing.T) {
	getStderr := captureStderr(t)

	forbiddenErr := &api.ForbiddenError{Message: "Your access token doesn't have the read_suites scope"}
	err := handleError(forbiddenErr)

	assert.Equal(t, forbiddenErr, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "❌ Access Denied:")
	assert.Contains(t, stderr, "Your access token doesn't have the read_suites scope")
}

func TestHandleError_NotFoundError(t *testing.T) {
	getStderr := captureStderr(t)

	notFoundErr := &api.NotFoundError{Message: "No suite found"}
	err := handleError(notFoundErr)

	assert.Nil(t, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "⚠️ Not Found:")
	assert.Contains(t, stderr, "No suite found")
	assert.Contains(t, stderr, "BUILDKITE_TEST_ENGINE_SUITE_SLUG")
	assert.Contains(t, stderr, "Falling back to non-intelligent splitting")
}

func TestHandleError_BadRequestError(t *testing.T) {
	getStderr := captureStderr(t)

	badReqErr := &api.BadRequestError{Message: "Invalid parameters"}
	err := handleError(badReqErr)

	assert.Equal(t, badReqErr, err)

	stderr := getStderr()
	assert.Contains(t, stderr, "❌ Invalid Request:")
	assert.Contains(t, stderr, "Invalid parameters")
}

func TestHandleError_UnknownError(t *testing.T) {
	getStderr := captureStderr(t)

	originalErr := fmt.Errorf("something unexpected")
	err := handleError(originalErr)

	assert.Equal(t, originalErr, err)

	stderr := getStderr()
	assert.Empty(t, stderr)
}

func TestWarnErrorPlan(t *testing.T) {
	getStderr := captureStderr(t)

	warnErrorPlan()

	stderr := getStderr()
	assert.Contains(t, stderr, "⚠️ Error Plan:")
	assert.Contains(t, stderr, "Server returned an error plan")
	assert.Contains(t, stderr, "Upload test results first")
	assert.Contains(t, stderr, "Falling back to non-intelligent splitting")
}
