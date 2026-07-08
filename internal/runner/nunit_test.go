package runner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/buildkite/test-engine-client/v2/internal/plan"
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

func TestNUnit_GetSelectors(t *testing.T) {
	changeCwd(t, "./testdata/nunit")

	nunit := NewNUnit(RunnerConfig{
		TestFilePattern: "tests/**/*Tests.cs",
	})

	got, err := nunit.GetSelectors()
	if err != nil {
		t.Errorf("NUnit.GetSelectors() error = %v", err)
	}

	want := []string{"MyLib.Tests.CalculatorTests", "MyLib.Tests.SimpleStackTests", "MyLib.Tests.StringUtilsTests"}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("NUnit.GetSelectors() diff (-got +want):\n%s", diff)
	}
}

// TestNUnit_GetSelectors_SameFilenameDifferentNamespace guards against the
// collision extractClassNames has: two files with the same filename but
// different namespaces used to collapse into a single, ambiguous class name.
// GetSelectors should keep them distinct by qualifying each with its
// namespace.
func TestNUnit_GetSelectors_SameFilenameDifferentNamespace(t *testing.T) {
	dir := t.TempDir()

	writeTestFile := func(t *testing.T, subdir, namespace string) {
		t.Helper()
		fullDir := filepath.Join(dir, "tests", subdir)
		if err := os.MkdirAll(fullDir, 0o755); err != nil {
			t.Fatalf("os.MkdirAll() error = %v", err)
		}
		content := "namespace " + namespace + ";\n\npublic class CalculatorTests {}\n"
		if err := os.WriteFile(filepath.Join(fullDir, "CalculatorTests.cs"), []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
	}

	writeTestFile(t, "MyLib.Tests", "MyLib.Tests")
	writeTestFile(t, "OtherLib.Tests", "OtherLib.Tests")

	changeCwd(t, dir)

	nunit := NewNUnit(RunnerConfig{
		TestFilePattern: "tests/**/*Tests.cs",
	})

	got, err := nunit.GetSelectors()
	if err != nil {
		t.Errorf("NUnit.GetSelectors() error = %v", err)
	}

	want := []string{"MyLib.Tests.CalculatorTests", "OtherLib.Tests.CalculatorTests"}

	sort.Strings(got)
	sort.Strings(want)

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("NUnit.GetSelectors() diff (-got +want):\n%s", diff)
	}
}

func TestNUnit_NamespacedClassNameForFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "block-scoped namespace",
			content: "using NUnit.Framework;\n\nnamespace MyLib.Tests\n{\n    public class CalculatorTests {}\n}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "file-scoped namespace",
			content: "using NUnit.Framework;\n\nnamespace MyLib.Tests;\n\npublic class CalculatorTests {}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "no namespace declared",
			content: "public class CalculatorTests {}\n",
			want:    "CalculatorTests",
		},
		{
			name:    "commented out namespace line is ignored",
			content: "// namespace NotTheRealNamespace;\nnamespace MyLib.Tests;\n\npublic class CalculatorTests {}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "nested block-scoped namespaces",
			content: "using NUnit.Framework;\n\nnamespace MyLib\n{\n    namespace Tests\n    {\n        public class CalculatorTests {}\n    }\n}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "sibling namespace after the target class is ignored",
			content: "namespace MyLib.Tests\n{\n    public class CalculatorTests {}\n}\n\nnamespace Helpers\n{\n    public class SomeHelper {}\n}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "sibling namespace before the target class is ignored",
			content: "namespace Helpers\n{\n    public class SomeHelper {}\n}\n\nnamespace MyLib.Tests\n{\n    public class CalculatorTests {}\n}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
		{
			name:    "UTF-8 BOM before a namespace on the first line",
			content: "\xEF\xBB\xBFnamespace MyLib.Tests;\n\npublic class CalculatorTests {}\n",
			want:    "MyLib.Tests.CalculatorTests",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := dir + "/CalculatorTests.cs"
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}

			got, err := namespacedClassNameForFile(path)
			if err != nil {
				t.Errorf("namespacedClassNameForFile() error = %v", err)
			}
			if got != c.want {
				t.Errorf("namespacedClassNameForFile() = %q, want %q", got, c.want)
			}
		})
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

func TestNUnit_ExtractClassNames_Selector(t *testing.T) {
	testCases := []plan.TestCase{
		{Format: plan.TestCaseFormatSelector, Value: "CalculatorTests"},
		{Format: plan.TestCaseFormatSelector, Value: "StringUtilsTests"},
		{Format: plan.TestCaseFormatSelector, Value: "CalculatorTests"}, // duplicate
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
		{
			classNames: []string{"MyLib.Tests.CalculatorTests"},
			want:       "FullyQualifiedName~MyLib.Tests.CalculatorTests",
		},
		{
			classNames: []string{"MyLib.Tests.CalculatorTests", "StringUtilsTests"},
			want:       "FullyQualifiedName~MyLib.Tests.CalculatorTests|FullyQualifiedName~.StringUtilsTests",
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

	classNames := []plan.TestCase{{Path: "CalculatorTests"}, {Path: "StringUtilsTests"}}

	gotName, gotArgs, err := nunit.CommandNameAndArgs(classNames, false)
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

func TestNUnit_CommandNameAndArgs_Selector(t *testing.T) {
	nunit := NewNUnit(RunnerConfig{
		ResultPath: "test-results.xml",
	})

	testCases := []plan.TestCase{
		{Format: plan.TestCaseFormatSelector, Value: "MyLib.Tests.CalculatorTests"},
		{Format: plan.TestCaseFormatSelector, Value: "MyLib.Tests.StringUtilsTests"},
	}

	gotName, gotArgs, err := nunit.CommandNameAndArgs(testCases, false)
	if err != nil {
		t.Errorf("commandNameAndArgs() error = %v", err)
	}

	wantName := "dotnet"
	wantArgs := []string{
		"test",
		"--no-build",
		"--filter",
		"FullyQualifiedName~MyLib.Tests.CalculatorTests|FullyQualifiedName~MyLib.Tests.StringUtilsTests",
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
	results, err := loadAndParseJUnitXML("./testdata/nunit/junit-results.xml")
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
