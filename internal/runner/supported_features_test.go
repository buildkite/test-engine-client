package runner

import "testing"

func TestSupportedFeatures_SplitBySelectorOnlyGotest(t *testing.T) {
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

	runners := []TestRunner{
		NewRspec(runnerConfig),
		NewJest(runnerConfig),
		NewVitest(runnerConfig),
		NewPlaywright(runnerConfig),
		NewCypress(runnerConfig),
		NewPytest(runnerConfig),
		NewPytestPants(runnerConfig),
		NewGoTest(runnerConfig),
		NewCucumber(runnerConfig),
		NewNUnit(runnerConfig),
		custom,
	}

	for _, runner := range runners {
		got := runner.SupportedFeatures().SplitBySelector
		want := runner.Name() == "gotest"
		if got != want {
			t.Errorf("%s SplitBySelector = %v, want %v", runner.Name(), got, want)
		}
	}
}
