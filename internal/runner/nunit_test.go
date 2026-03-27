package runner

import (
	"testing"

	"github.com/buildkite/test-engine-client/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestNewNUnit(t *testing.T) {
	cases := []struct {
		input RunnerConfig
		want  RunnerConfig
	}{
		{
			input: RunnerConfig{},
			want: RunnerConfig{
				TestCommand:      "dotnet test --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}",
				TestFilePattern:  "**/*Tests.cs",
				RetryTestCommand: "dotnet test --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}",
			},
		},
		{
			input: RunnerConfig{
				TestCommand:     "dotnet test --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}",
				TestFilePattern: "tests/**/*Tests.cs",
			},
			want: RunnerConfig{
				TestCommand:      "dotnet test --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}",
				TestFilePattern:  "tests/**/*Tests.cs",
				RetryTestCommand: "dotnet test --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}",
			},
		},
	}

	for _, c := range cases {
		got := NewNUnit(c.input)
		if diff := cmp.Diff(got.RunnerConfig, c.want, cmp.AllowUnexported(RunnerConfig{})); diff != "" {
			t.Errorf("NewNUnit(%v) diff (-got +want):\n%s", c.input, diff)
		}
	}
}

func TestNUnit_GetFiles(t *testing.T) {
	changeCwd(t, "./testdata/nunit")

	nunit := NewNUnit(RunnerConfig{
		TestFilePattern: "tests/**/*Tests.cs",
	})

	got, err := nunit.GetFiles()
	if err != nil {
		t.Errorf("NUnit.GetFiles() error = %v", err)
	}

	want := []string{
		"tests/MyLib.Tests/CalculatorTests.cs",
		"tests/MyLib.Tests/SimpleStackTests.cs",
		"tests/MyLib.Tests/StringUtilsTests.cs",
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("NUnit.GetFiles() diff (-got +want):\n%s", diff)
	}
}

func TestNUnit_GetExamples(t *testing.T) {
	nunit := NewNUnit(RunnerConfig{})
	_, err := nunit.GetExamples([]string{"tests/MyLib.Tests/CalculatorTests.cs"})
	if err == nil || err.Error() != "not supported in NUnit" {
		t.Errorf("GetExamples() error = %v, want %q", err, "not supported in NUnit")
	}
}

func TestNUnit_ExtractClassNames(t *testing.T) {
	testCases := []plan.TestCase{
		{Path: "tests/MyLib.Tests/CalculatorTests.cs"},
		{Path: "tests/MyLib.Tests/StringUtilsTests.cs"},
		{Path: "tests/MyLib.Tests/CalculatorTests.cs"}, // duplicate
	}

	got := extractClassNames(testCases)
	want := []string{"CalculatorTests", "StringUtilsTests"}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("extractClassNames() diff (-got +want):\n%s", diff)
	}
}

func TestNUnit_BuildTestFilter(t *testing.T) {
	cases := []struct {
		classNames []string
		want       string
	}{
		{
			classNames: []string{"CalculatorTests"},
			want:       "FullyQualifiedName~.CalculatorTests",
		},
		{
			classNames: []string{"CalculatorTests", "StringUtilsTests"},
			want:       "FullyQualifiedName~.CalculatorTests|FullyQualifiedName~.StringUtilsTests",
		},
	}

	for _, c := range cases {
		got := buildTestFilter(c.classNames)
		if got != c.want {
			t.Errorf("buildTestFilter(%v) = %q, want %q", c.classNames, got, c.want)
		}
	}
}

func TestNUnit_CommandNameAndArgs(t *testing.T) {
	nunit := NewNUnit(RunnerConfig{
		ResultPath: "test-results.xml",
	})

	classNames := []string{"CalculatorTests", "StringUtilsTests"}

	gotName, gotArgs, err := nunit.commandNameAndArgs(nunit.TestCommand, classNames)
	if err != nil {
		t.Errorf("commandNameAndArgs() error = %v", err)
	}

	wantName := "dotnet"
	wantArgs := []string{
		"test",
		"--no-build",
		"--filter",
		"FullyQualifiedName~.CalculatorTests|FullyQualifiedName~.StringUtilsTests",
		"--logger",
		"junit;LogFilePath=test-results.xml",
	}

	if gotName != wantName {
		t.Errorf("commandNameAndArgs() name = %v, want %v", gotName, wantName)
	}

	if diff := cmp.Diff(gotArgs, wantArgs); diff != "" {
		t.Errorf("commandNameAndArgs() args diff (-got +want):\n%s", diff)
	}
}

func TestNUnit_ParseJUnitResults(t *testing.T) {
	results, err := loadAndParseJUnitXmlResult("./testdata/nunit/junit-results.xml")
	if err != nil {
		t.Fatalf("loadAndParseJUnitXmlResult() error = %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("loadAndParseJUnitXmlResult() len = %d, want 5", len(results))
	}

	// Check passed test
	if results[0].Name != "AddTwoNumbers" || results[0].Result != TestStatusPassed {
		t.Errorf("results[0] = {Name: %q, Result: %q}, want {Name: \"AddTwoNumbers\", Result: \"passed\"}", results[0].Name, results[0].Result)
	}
	if results[0].Classname != "MyLib.Tests.CalculatorTests" {
		t.Errorf("results[0].Classname = %q, want %q", results[0].Classname, "MyLib.Tests.CalculatorTests")
	}

	// Check failed test
	if results[2].Name != "DivideByZero" || results[2].Result != TestStatusFailed {
		t.Errorf("results[2] = {Name: %q, Result: %q}, want {Name: \"DivideByZero\", Result: \"failed\"}", results[2].Name, results[2].Result)
	}

	// Check skipped test
	if results[4].Name != "SkippedTest" || results[4].Result != TestStatusSkipped {
		t.Errorf("results[4] = {Name: %q, Result: %q}, want {Name: \"SkippedTest\", Result: \"skipped\"}", results[4].Name, results[4].Result)
	}
}
