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
	pytestPants := NewPytestPants(runnerConfig)
	gotest := NewGoTest(runnerConfig)
	cucumber := NewCucumber(runnerConfig)
	nunit := NewNUnit(runnerConfig)

	runners := []TestRunner{
		custom,
		rspec,
		jest,
		playwright,
		cypress,
		pytest,
		pytestPants,
		gotest,
		cucumber,
		nunit,
	}

	supportedRunners := []string{gotest.Name(), custom.Name()}

	for _, runner := range runners {
		got := runner.SupportedFeatures().SplitBySelector
		want := slices.Contains(supportedRunners, runner.Name())
		if got != want {
			t.Errorf("%s SplitBySelector = %v, want %v", runner.Name(), got, want)
		}
	}
}
