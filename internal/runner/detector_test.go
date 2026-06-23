package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmitSelectorTagsForTestEngineClient(t *testing.T) {
	t.Setenv(temporaryTestEngineClientSelectorTagsEnv, "true")
	t.Setenv("BUILDKITE_ORGANIZATION_SLUG", "buildkite")
	t.Setenv("BUILDKITE_PIPELINE_SLUG", "test-engine-client")
	assert.True(t, emitSelectorTagsForTestEngineClient())

	t.Setenv("BUILDKITE_PIPELINE_SLUG", "other-pipeline")
	assert.False(t, emitSelectorTagsForTestEngineClient())
}
