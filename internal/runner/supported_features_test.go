package runner

import (
	"slices"
	"testing"
)

func TestSupportedFeatures_SplitBySelectorSupportedRunners(t *testing.T) {
	runnerConfig := RunnerConfig{
		ResultPath:       "results.json",
		TestCommand:      "test-command",
		TestFilePattern:  "**/*_test.go",
		RetryTestCommand: "retry-command",
	}

	custom, err := NewCustom(runnerConfig)
	if err != nil {
		t.Fatalf("NewCustom() error = %v", err)
	}

	rspec := NewRspec(runnerConfig)
	jest := NewJest(runnerConfig)
	playwright := NewPlaywright(runnerConfig)
	cypress := NewCypress(runnerConfig)
	pytest := NewPytest(runnerConfig)
	gotest := NewGoTest(runnerConfig)
	cucumber := NewCucumber(runnerConfig)
	vitest := NewVitest(runnerConfig)

	runners := []TestRunner{
		custom,
		rspec,
		jest,
		playwright,
		cypress,
		pytest,
		gotest,
		cucumber,
		vitest,
	}

	supportedRunners := []string{
		gotest.Name(),
		rspec.Name(),
		custom.Name(),
		jest.Name(),
		playwright.Name(),
		cypress.Name(),
		pytest.Name(),
		cucumber.Name(),
		vitest.Name(),
	}

	for _, runner := range runners {
		got := runner.SupportedFeatures().SplitBySelector
		want := slices.Contains(supportedRunners, runner.Name())
		if got != want {
			t.Errorf("%s SplitBySelector = %v, want %v", runner.Name(), got, want)
		}
	}
}

func TestSupportedFeatures_SplitByExampleRequiresExampleDiscoverer(t *testing.T) {
	runnerConfig := RunnerConfig{
		ResultPath:      "results.json",
		TestCommand:     "test-command",
		TestFilePattern: "**/*_test.go",
	}
	custom, err := NewCustom(runnerConfig)
	if err != nil {
		t.Fatalf("NewCustom() error = %v", err)
	}

	runners := []TestRunner{
		custom,
		NewRspec(runnerConfig),
		NewJest(runnerConfig),
		NewPlaywright(runnerConfig),
		NewCypress(runnerConfig),
		NewPytest(runnerConfig),
		NewGoTest(runnerConfig),
		NewCucumber(runnerConfig),
		NewVitest(runnerConfig),
	}

	for _, testRunner := range runners {
		_, implementsExampleDiscovery := testRunner.(ExampleDiscoverer)
		if got := testRunner.SupportedFeatures().SplitByExample; got != implementsExampleDiscovery {
			t.Errorf("%s SplitByExample = %v, implements ExampleDiscoverer = %v", testRunner.Name(), got, implementsExampleDiscovery)
		}
	}
}
