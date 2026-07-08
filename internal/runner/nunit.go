package runner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/buildkite/test-engine-client/v2/internal/debug"
	"github.com/buildkite/test-engine-client/v2/internal/plan"
	"github.com/kballard/go-shellquote"
)

// utf8BOM is the byte sequence Visual Studio commonly writes at the start of
// a .cs file. Go's regexp \s class doesn't treat it as whitespace, so a
// namespace declaration on the file's first line would otherwise fail to
// match namespaceDeclarationPattern's "^" anchor.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type NUnit struct {
	RunnerConfig
}

// Compile-time check that NUnit implements TestRunner
var _ TestRunner = (*NUnit)(nil)

func (n NUnit) SupportedFeatures() SupportedFeatures {
	return SupportedFeatures{
		SplitByFile:     true,
		SplitByExample:  false,
		SplitBySelector: true,
		FilterTestFiles: true,
		FilterTestByTag: false,
		AutoRetry:       true,
		Mute:            true,
		Skip:            false,
	}
}

func NewNUnit(c RunnerConfig) NUnit {
	if c.TestCommand == "" {
		c.TestCommand = "dotnet test --no-build --filter {{testFilter}} --logger junit;LogFilePath={{resultPath}}"
	}

	if c.TestFilePattern == "" {
		c.TestFilePattern = "**/*Tests.cs"
	}

	if c.RetryTestCommand == "" {
		c.RetryTestCommand = c.TestCommand
	}

	return NUnit{
		RunnerConfig: c,
	}
}

func (n NUnit) Name() string {
	return "NUnit"
}

func (n NUnit) ResultFormat() string {
	return "junit"
}

// GetFiles returns an array of .cs test file names using the discovery pattern.
func (n NUnit) GetFiles() ([]string, error) {
	debug.Println("Discovering test files with include pattern:", n.TestFilePattern, "exclude pattern:", n.TestFileExcludePattern)
	files, err := discoverTestFiles(n.TestFilePattern, n.TestFileExcludePattern)
	debug.Println("Discovered", len(files), "files")

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found with pattern %q and exclude pattern %q", n.TestFilePattern, n.TestFileExcludePattern)
	}

	return files, nil
}

func (n NUnit) GetExamples(files []string) ([]plan.TestCase, error) {
	return nil, fmt.Errorf("not supported in NUnit")
}

// namespaceDeclarationPattern matches a C# namespace declaration at the start
// of a line, in either the block-scoped ("namespace Foo.Bar {") or
// file-scoped ("namespace Foo.Bar;") form.
var namespaceDeclarationPattern = regexp.MustCompile(`(?m)^\s*namespace\s+([\w.]+)\s*[{;]`)

// GetSelectors returns the namespace-qualified class names discovered from the
// test files, for use as the explicit selector list sent to the test plan
// API. The namespace is read from each file's `namespace` declaration, since
// that's the value NUnit itself writes as the JUnit `classname` attribute
// (and what ta-ingestion attributes as test.selector.primary), so the
// selectors bktec sends line up with the historical duration data keyed on
// that same value.
func (n NUnit) GetSelectors() ([]string, error) {
	files, err := n.GetFiles()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	selectors := make([]string, 0, len(files))
	for _, file := range files {
		className, err := namespacedClassNameForFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to determine class name for %q: %w", file, err)
		}
		if !seen[className] {
			selectors = append(selectors, className)
			seen[className] = true
		}
	}

	return selectors, nil
}

// namespacedClassNameForFile derives the namespace-qualified class name for a
// .cs file, e.g. "MyLib.Tests.CalculatorTests" for a file declaring
// "namespace MyLib.Tests" and relying on the documented NUnit convention (see
// docs/nunit.md) that each file contains a single class matching its
// filename. Files with no enclosing namespace declaration (the global
// namespace, or the class can't be located) fall back to the bare class name.
func namespacedClassNameForFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content = bytes.TrimPrefix(content, utf8BOM)

	className := strings.TrimSuffix(filepath.Base(path), ".cs")

	classIndex := len(content)
	if loc := classDeclarationPattern(className).FindIndex(content); loc != nil {
		classIndex = loc[0]
	}

	segments := append(enclosingNamespaceSegments(content, classIndex), className)

	return strings.Join(segments, "."), nil
}

// classDeclarationPattern builds a pattern matching a "class ClassName"
// declaration, used to locate the byte offset the enclosing namespace lookup
// should stop at.
func classDeclarationPattern(className string) *regexp.Regexp {
	return regexp.MustCompile(`\bclass\s+` + regexp.QuoteMeta(className) + `\b`)
}

// enclosingNamespaceSegments walks the namespace declarations before
// byte offset upTo, tracking brace depth, and returns the segments of the
// ones still open at that point, in outer-to-inner order.
//
// This scopes the lookup to namespaces that actually enclose the target
// class, rather than every namespace declared anywhere in the file: a sibling
// namespace declared before or after the target class's block (e.g.
// "namespace MyLib.Tests { class CalculatorTests {} } namespace Helpers {}")
// opens and/or closes outside the target's brace depth, so it's excluded.
// Nested block-scoped namespaces (e.g. "namespace MyLib { namespace Tests {
// ... } }") are still collected in order and joined, since each contributes
// its own segment to the fully qualified name.
//
// Brace depth is tracked by counting every "{"/"}" byte in the file, without
// understanding comments or string literals, which mirrors the level of
// fragility already accepted by namespaceDeclarationPattern itself.
func enclosingNamespaceSegments(content []byte, upTo int) []string {
	type frame struct {
		depth   int
		segment string
	}

	var stack []frame
	depth := 0
	pos := 0

	popClosed := func() {
		for len(stack) > 0 && depth <= stack[len(stack)-1].depth {
			stack = stack[:len(stack)-1]
		}
	}

	for _, m := range namespaceDeclarationPattern.FindAllSubmatchIndex(content, -1) {
		start, end := m[0], m[1]
		if start >= upTo {
			break
		}

		depth += braceDelta(content[pos:start])
		popClosed()

		// A block-scoped namespace ("namespace X {") closes when depth drops
		// back to its pre-block depth, so record that depth. A file-scoped
		// namespace ("namespace X;") has no closing brace and applies for
		// the rest of the file, so use a depth no valid brace nesting can
		// ever drop back to, keeping it on the stack permanently.
		frameDepth := depth
		if content[end-1] == '{' {
			depth++
		} else {
			frameDepth = -1
		}
		stack = append(stack, frame{depth: frameDepth, segment: string(content[m[2]:m[3]])})
		pos = end
	}

	depth += braceDelta(content[pos:upTo])
	popClosed()

	segments := make([]string, len(stack))
	for i, f := range stack {
		segments[i] = f.segment
	}
	return segments
}

// braceDelta counts the net change in brace depth across b.
func braceDelta(b []byte) int {
	delta := 0
	for _, c := range b {
		switch c {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

// Run executes dotnet test with a --filter expression built from the test cases.
// Test cases are mapped from .cs file paths to class names, and joined into a
// FullyQualifiedName~ filter expression.
func (n NUnit) Run(result *RunResult, testCases []plan.TestCase, retry bool) error {
	cmd, err := buildCommand(n, testCases, retry)
	if err != nil {
		return err
	}

	cmdErr := runAndForwardSignal(cmd)

	testResults, parseErr := loadAndParseJUnitXML(n.ResultPath)
	if parseErr != nil {
		fmt.Printf("Buildkite Test Engine Client: Failed to read NUnit output, tests will not be retried: %v\n", parseErr)
		return cmdErr
	}

	for _, test := range testResults {
		result.RecordTestResult(plan.TestCase{
			Scope: test.Classname,
			Name:  test.Name,
			Path:  test.Classname,
		}, test.Result)
	}

	return cmdErr
}

// extractClassNames extracts unique class names from test cases.
// For selector-based test cases, the class name is already resolved in
// tc.Value. For file-based test cases, tc.Path is expected to be a .cs file
// path like "tests/MyLib.Tests/CalculatorTests.cs", and the class name is the
// filename without extension, e.g. "CalculatorTests".
func extractClassNames(testCases []plan.TestCase) []string {
	seen := map[string]bool{}
	var classNames []string

	for _, tc := range testCases {
		className := classNameFromTestCase(tc)
		if !seen[className] {
			classNames = append(classNames, className)
			seen[className] = true
		}
	}

	return classNames
}

// classNameFromTestCase resolves the NUnit class name for a single test case,
// mirroring how gotest's packageFromTestCase resolves a package name: use the
// explicit selector value when the test plan already gave us one, otherwise
// derive it from the .cs file path.
func classNameFromTestCase(tc plan.TestCase) string {
	if tc.Format == plan.TestCaseFormatSelector {
		return tc.Value
	}
	return strings.TrimSuffix(filepath.Base(tc.Path), ".cs")
}

// buildTestFilter constructs a dotnet test --filter expression from class names.
// Each class name becomes a "FullyQualifiedName~" predicate, joined with "|" (OR).
// The "~" operator is a "contains" match.
//
// Namespace-qualified class names (e.g. "MyLib.Tests.CalculatorTests") already
// spell out the full path from the root namespace, so they're matched as-is;
// a leading "." would never match, since there's no namespace segment before
// the root one. Bare class names (e.g. "CalculatorTests") get a leading "."
// to anchor to a namespace/class boundary, preventing false positives on
// partial name matches (e.g. "SubCalculatorTests").
func buildTestFilter(classNames []string) string {
	parts := make([]string, len(classNames))
	for i, name := range classNames {
		if strings.Contains(name, ".") {
			parts[i] = fmt.Sprintf("FullyQualifiedName~%s", name)
		} else {
			parts[i] = fmt.Sprintf("FullyQualifiedName~.%s", name)
		}
	}
	return strings.Join(parts, "|")
}

func (n NUnit) CommandNameAndArgs(testCases []plan.TestCase, retry bool) (string, []string, error) {
	classNames := extractClassNames(testCases)

	cmd := n.TestCommand
	if retry {
		cmd = n.RetryTestCommand
	}

	filter := buildTestFilter(classNames)

	cmd = strings.Replace(cmd, "{{testFilter}}", filter, 1)
	cmd = strings.Replace(cmd, "{{resultPath}}", n.ResultPath, 1)

	words, err := shellquote.Split(cmd)
	if err != nil {
		return "", []string{}, err
	}

	return words[0], words[1:], nil
}
