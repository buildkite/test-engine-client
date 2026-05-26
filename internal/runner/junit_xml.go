package runner

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
)

// JUnitXMLTestCase represents a single <testcase> element in JUnit XML.
type JUnitXMLTestCase struct {
	Classname string           `xml:"classname,attr"`
	Name      string           `xml:"name,attr"`
	Result    TestStatus
	Failure   *JUnitXMLFailure `xml:"failure"`
	Skipped   *JUnitXMLSkipped `xml:"skipped"`
}

type junitXMLTestSuite struct {
	XMLName   xml.Name           `xml:"testsuite"`
	Name      string             `xml:"name,attr"`
	TestCases []JUnitXMLTestCase `xml:"testcase"`
}

type junitXMLTestSuites struct {
	XMLName    xml.Name            `xml:"testsuites"`
	TestSuites []junitXMLTestSuite `xml:"testsuite"`
}

func loadAndParseJUnitXML(path string) ([]JUnitXMLTestCase, error) {
	xmlFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open JUnit XML file %s: %w", path, err)
	}
	defer xmlFile.Close()

	byteValue, err := io.ReadAll(xmlFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read JUnit XML file %s: %w", path, err)
	}

	var testSuites junitXMLTestSuites
	if err = xml.Unmarshal(byteValue, &testSuites); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JUnit XML file %s: %w", path, err)
	}

	var results []JUnitXMLTestCase
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
