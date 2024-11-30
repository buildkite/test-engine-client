package testdata

import "testing"

type TestSuite struct {
	t *testing.T
}

func (s *TestSuite) SetupTest() {
	// Setup code
}

func (s *TestSuite) TeardownTest() {
	// Teardown code
}

func TestSuiteA(t *testing.T) {
	suite := &TestSuite{t: t}
	suite.SetupTest()
	defer suite.TeardownTest()
	// Test code
}

func TestSuiteB(t *testing.T) {
	suite := &TestSuite{t: t}
	suite.SetupTest()
	defer suite.TeardownTest()
	t.Error("This suite test should fail")
}
