package main

import (
	"fmt"

	"github.com/buildkite/test-engine-client/v2/internal/runner"
)

func printTableRow(cols ...string) {
	for _, col := range cols {
		fmt.Printf("| %s ", col)
	}
	fmt.Printf("|\n")
}

func boolToEmoji(b bool) string {
	if b {
		return "✅"
	} else {
		return "❌"
	}
}

func printRow(runners []runner.TestRunner, featureName string, featureValueFunc func(runner.TestRunner) bool) {
	var values []string
	values = append(values, featureName)
	for _, runner := range runners {
		values = append(values, boolToEmoji(featureValueFunc(runner)))
	}
	printTableRow(values...)
}

func main() {
	runnerConfig := runner.RunnerConfig{
		TestCommand: "foo",
	}

	// NewCustom() method signature is incompatible with the other runners,
	// drop the second return value
	custom, _ := runner.NewCustom(runnerConfig)

	runners := []runner.TestRunner{
		runner.NewRspec(runnerConfig),
		runner.NewJest(runnerConfig),
		runner.NewPlaywright(runnerConfig),
		runner.NewCypress(runnerConfig),
		runner.NewPytest(runnerConfig),
		runner.NewPytestPants(runnerConfig),
		runner.NewGoTest(runnerConfig),
		runner.NewCucumber(runnerConfig),
		runner.NewNUnit(runnerConfig),
		custom,
	}

	headings := []string{"Feature"}
	for _, runner := range runners {
		headings = append(headings, runner.Name())
	}
	printTableRow(headings...)

	separators := []string{"---"}
	for range runners {
		separators = append(separators, ":---:")
	}
	printTableRow(separators...)

	printRow(
		runners,
		"Split tests by file[^1]",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().SplitByFile },
	)

	printRow(
		runners,
		"[Split slow files by individual test example](https://github.com/buildkite/test-engine-client/blob/main/docs/rspec.md#split-slow-files-by-individual-test-example)",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().SplitByExample },
	)

	printRow(
		runners,
		"Filter test files",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().FilterTestFiles },
	)

	printRow(
		runners,
		"Filter tests by tag",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().FilterTestByTag },
	)

	printRow(
		runners,
		"Automatically retry failed test",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().AutoRetry },
	)

	printRow(
		runners,
		"Mute tests (ignore test failures)",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().Mute },
	)

	printRow(
		runners,
		"Skip tests",
		func(r runner.TestRunner) bool { return r.SupportedFeatures().Skip },
	)
}
