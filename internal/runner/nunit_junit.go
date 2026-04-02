package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

// NUnitJUnitResult represents a single test case from a JUnit XML report
// produced by dotnet test --logger junit.
type NUnitJUnitResult struct {
	Classname string           `xml:"classname,attr"`
	Name      string           `xml:"name,attr"`
	Result    TestStatus       // passed | failed | skipped
	Failure   *JUnitXMLFailure `xml:"failure"`
	Skipped   *JUnitXMLSkipped `xml:"skipped"`
}

// nunitJUnitTestSuite represents a <testsuite> element in JUnit XML from dotnet test.
type nunitJUnitTestSuite struct {
	XMLName   xml.Name           `xml:"testsuite"`
	Name      string             `xml:"name,attr"`
	TestCases []NUnitJUnitResult `xml:"testcase"`
}

// nunitJUnitTestSuites represents the root <testsuites> element in JUnit XML from dotnet test.
type nunitJUnitTestSuites struct {
	XMLName    xml.Name              `xml:"testsuites"`
	TestSuites []nunitJUnitTestSuite `xml:"testsuite"`
}

func loadAndParseJUnitXmlResult(path string) ([]NUnitJUnitResult, error) {
	xmlFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open JUnit XML file %s: %w", path, err)
	}
	defer xmlFile.Close()

	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read JUnit XML file %s: %w", path, err)
	}

	var testSuites nunitJUnitTestSuites
	err = xml.Unmarshal(byteValue, &testSuites)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JUnit XML file %s: %w", path, err)
	}

	var results []NUnitJUnitResult
	for _, suite := range testSuites.TestSuites {
		for _, tc := range suite.TestCases {
			testCase := tc
			if testCase.Failure != nil {
				testCase.Result = TestStatusFailed
			} else if testCase.Skipped != nil {
				testCase.Result = TestStatusSkipped
			} else {
				testCase.Result = TestStatusPassed
			}
			results = append(results, testCase)
		}
	}

	return results, nil
}
