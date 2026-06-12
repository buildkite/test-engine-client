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
	Result    TestStatus       // passed | failed | skipped
	Failure   *JUnitXMLFailure `xml:"failure"`
	Error     *JUnitXMLError   `xml:"error"`
	Skipped   *JUnitXMLSkipped `xml:"skipped"`
	// SuiteName is the name attribute of the enclosing <testsuite> element.
	SuiteName string `xml:"-"`
}

// JUnitXMLFailure represents the <failure> element in JUnit XML
type JUnitXMLFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitXMLError represents the <error> element in JUnit XML
type JUnitXMLError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitXMLSkipped represents the <skipped> element in JUnit XML
type JUnitXMLSkipped struct {
	Message string `xml:"message,attr"`
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
	err = xml.Unmarshal(byteValue, &testSuites)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JUnit XML file %s: %w", path, err)
	}

	var results []JUnitXMLTestCase
	for _, suite := range testSuites.TestSuites {
		for _, tc := range suite.TestCases {
			testCase := tc
			testCase.SuiteName = suite.Name
			if testCase.Failure != nil || testCase.Error != nil {
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
