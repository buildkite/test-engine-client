package runner

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exampleJUnitXML = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="4" failures="1" errors="0" time="1.639386">
	<testsuite tests="4" failures="1" time="1.306000" name="github.com/buildkite/test-engine-client/v2/internal/debug" timestamp="2025-04-22T15:34:03+10:00">
		<properties>
			<property name="go.version" value="go1.24.1 darwin/arm64"></property>
		</properties>
		<testcase classname="github.com/buildkite/test-engine-client/v2/internal/debug" name="TestPrintf" time="0.000000">
			<failure message="Failed" type="">=== RUN   TestPrintf&#xA;    debug_test.go:21: error matching output: &lt;nil&gt;&#xA;--- FAIL: TestPrintf (0.00s)&#xA;</failure>
		</testcase>
		<testcase classname="github.com/buildkite/test-engine-client/v2/internal/debug" name="TestPrintf_disabled" time="1.000000"></testcase>
		<testcase classname="github.com/buildkite/test-engine-client/v2/internal/debug" name="TestPrintln" time="0.000000"></testcase>
		<testcase classname="github.com/buildkite/test-engine-client/v2/internal/debug" name="TestPrintln_disabled" time="0.000000"></testcase>
	</testsuite>
</testsuites>`

const exampleJUnitXMLWithError = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="2" failures="0" errors="1" time="0.5">
	<testsuite tests="2" failures="0" errors="1" time="0.5" name="mypackage">
		<testcase classname="mypackage" name="TestSetupCrash" time="0.000000">
			<error message="panic in setup" type="RuntimeError">goroutine 1 [running]: panic...</error>
		</testcase>
		<testcase classname="mypackage" name="TestOK" time="0.000000"></testcase>
	</testsuite>
</testsuites>`

func TestLoadAndParseJUnitXML(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "junit.*.xml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name()) // clean up

	_, err = tmpfile.WriteString(exampleJUnitXML)
	require.NoError(t, err)
	err = tmpfile.Close()
	require.NoError(t, err)

	results, err := loadAndParseJUnitXML(tmpfile.Name())
	require.NoError(t, err)

	require.Len(t, results, 4)

	assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/debug", results[0].Classname)
	assert.Equal(t, "TestPrintf", results[0].Name)
	assert.Equal(t, TestStatusFailed, results[0].Result)
	assert.NotNil(t, results[0].Failure)
	assert.Nil(t, results[0].Skipped)

	assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/debug", results[1].Classname)
	assert.Equal(t, "TestPrintf_disabled", results[1].Name)
	assert.Equal(t, TestStatusPassed, results[1].Result)
	assert.Nil(t, results[1].Failure)
	assert.Nil(t, results[1].Skipped)

	assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/debug", results[2].Classname)
	assert.Equal(t, "TestPrintln", results[2].Name)
	assert.Equal(t, TestStatusPassed, results[2].Result)
	assert.Nil(t, results[2].Failure)
	assert.Nil(t, results[2].Skipped)

	assert.Equal(t, "github.com/buildkite/test-engine-client/v2/internal/debug", results[3].Classname)
	assert.Equal(t, "TestPrintln_disabled", results[3].Name)
	assert.Equal(t, TestStatusPassed, results[3].Result)
	assert.Nil(t, results[3].Failure)
	assert.Nil(t, results[3].Skipped)
}

func TestLoadAndParseJUnitXML_Error(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "junit.*.xml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.WriteString(exampleJUnitXMLWithError)
	require.NoError(t, err)
	err = tmpfile.Close()
	require.NoError(t, err)

	results, err := loadAndParseJUnitXML(tmpfile.Name())
	require.NoError(t, err)

	require.Len(t, results, 2)

	assert.Equal(t, "TestSetupCrash", results[0].Name)
	assert.Equal(t, TestStatusFailed, results[0].Result)
	assert.Nil(t, results[0].Failure)
	assert.NotNil(t, results[0].Error)
	assert.Equal(t, "panic in setup", results[0].Error.Message)
	assert.Equal(t, "RuntimeError", results[0].Error.Type)

	assert.Equal(t, "TestOK", results[1].Name)
	assert.Equal(t, TestStatusPassed, results[1].Result)
	assert.Nil(t, results[1].Failure)
	assert.Nil(t, results[1].Error)
}
