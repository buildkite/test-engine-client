package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

// Struct to decode gotestsun --junitfile=...
type GoTestJUnitResult struct {
	Classname string           `xml:"classname,attr"`
	Name      string           `xml:"name,attr"`
	Result    TestStatus       // passed | failed | skipped
	Failure   *JUnitXMLFailure `xml:"failure"`
	Skipped   *JUnitXMLSkipped `xml:"skipped"`
}

// JUnitXMLFailure represents the <failure> element in JUnit XML
type JUnitXMLFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitXMLSkipped represents the <skipped> element in JUnit XML
type JUnitXMLSkipped struct {
	Message string `xml:"message,attr"`
}

// JUnitXMLTestSuite represents the <testsuite> element in JUnit XML
type JUnitXMLTestSuite struct {
	XMLName   xml.Name            `xml:"testsuite"`
	Name      string              `xml:"name,attr"`
	Tests     int                 `xml:"tests,attr"`
	Failures  int                 `xml:"failures,attr"`
	Errors    int                 `xml:"errors,attr"`
	Time      float64             `xml:"time,attr"`
	Timestamp string              `xml:"timestamp,attr"`
	TestCases []GoTestJUnitResult `xml:"testcase"`
}

// JUnitXMLTestSuites represents the root <testsuites> element in JUnit XML
type JUnitXMLTestSuites struct {
	XMLName    xml.Name            `xml:"testsuites"`
	Tests      int                 `xml:"tests,attr"`
	Failures   int                 `xml:"failures,attr"`
	Errors     int                 `xml:"errors,attr"`
	Time       float64             `xml:"time,attr"`
	TestSuites []JUnitXMLTestSuite `xml:"testsuite"`
}

func loadAndParseGotestJUnitXmlResult(path string) ([]GoTestJUnitResult, error) {
	xmlFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open JUnit XML file %s: %w", path, err)
	}
	defer xmlFile.Close()

	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read JUnit XML file %s: %w", path, err)
	}

	var testSuites JUnitXMLTestSuites
	err = xml.Unmarshal(byteValue, &testSuites)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JUnit XML file %s: %w", path, err)
	}

	var results []GoTestJUnitResult
	for _, suite := range testSuites.TestSuites {
		for _, tc := range suite.TestCases {
			testCase := tc // Create a new variable to avoid closure capturing the loop variable
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
