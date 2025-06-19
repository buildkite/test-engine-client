//nolint:all
package runner

import (
	"encoding/json"
	"fmt"
	"os"
)

// CucumberFeature represents a single feature in Cucumber's JSON output.
type CucumberFeature struct {
	URI         string            `json:"uri"`
	ID          string            `json:"id"`
	Keyword     string            `json:"keyword"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Line        int               `json:"line"`
	Elements    []CucumberElement `json:"elements"`
	Tags        []CucumberTag     `json:"tags,omitempty"`
}

// CucumberElement represents a scenario or background in Cucumber's JSON output.
type CucumberElement struct {
	ID          string         `json:"id"`
	Keyword     string         `json:"keyword"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Line        int            `json:"line"`
	Type        string         `json:"type"` // e.g., "scenario", "background"
	Steps       []CucumberStep `json:"steps"`
	Tags        []CucumberTag  `json:"tags,omitempty"`
	// Examples    []CucumberExample `json:"examples,omitempty"` // For scenario outlines
}

// AggregatedStatus returns overall scenario status based on its steps.
// It mirrors the logic previously in cucumber.go but uses the parser's structs.
func (e CucumberElement) AggregatedStatus() string {
	// If there's no result for a step (e.g. in a dry run for GetExamples), it shouldn't affect status.
	// The primary use of AggregatedStatus is after a real run.
	status := "passed"
	for _, step := range e.Steps {
		if step.Result == nil {
			// A step with no result (e.g. from a dry run) shouldn't alter a 'passed' status
			// unless other steps explicitly fail or are skipped. If all steps have no result,
			// it's effectively passed from an aggregation perspective for determining if *any* step failed.
			continue
		}
		switch step.Result.Status {
		case "failed", "undefined", "errored": // 'errored' is a common status for unexpected issues
			return "failed"
		case "pending", "skipped":
			// If a scenario has both skipped and passed steps, it's considered skipped.
			// 'pending' takes precedence over 'passed' but 'failed' takes precedence over 'pending'.
			if status != "failed" { // don't downgrade from failed
				status = "pending" // Treat as pending/skipped
			}
		case "passed":
			// If current status is 'passed', it remains 'passed'.
			// If current status is 'pending', it remains 'pending'.
			// No change needed here if step is passed.
		default:
			// Unknown status, treat cautiously, but for now, don't alter current aggregate status
			// unless it's an explicit failure/skip.
		}
	}
	return status
}

// CucumberStep represents a single step in a scenario.
type CucumberStep struct {
	Keyword       string                 `json:"keyword"`
	Name          string                 `json:"name"`
	Line          int                    `json:"line"`
	Result        *CucumberResult        `json:"result,omitempty"`
	Match         *CucumberMatch         `json:"match,omitempty"`
	DocString     *CucumberDocString     `json:"doc_string,omitempty"`
	DataTableRows []CucumberDataTableRow `json:"rows,omitempty"` // For data tables
}

// CucumberResult represents the result of a step execution.
type CucumberResult struct {
	Status       string `json:"status"` // e.g., "passed", "failed", "skipped", "undefined"
	ErrorMessage string `json:"error_message,omitempty"`
	Duration     int64  `json:"duration,omitempty"` // Nanoseconds
}

// CucumberMatch represents the matching step definition for a step.
type CucumberMatch struct {
	Location string `json:"location"` // e.g., "features/step_definitions/steps.rb:5"
}

// CucumberTag represents a tag in Cucumber.
type CucumberTag struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// CucumberDocString represents a doc string argument for a step.
type CucumberDocString struct {
	Value       string `json:"value"`
	ContentType string `json:"content_type"`
	Line        int    `json:"line"`
}

// CucumberDataTableRow represents a row in a step's data table.
type CucumberDataTableRow struct {
	Cells []string `json:"cells"`
}

// ParseCucumberJSONReport parses the JSON output from a cucumber run (not dry run).
// This is for actual test results.
func ParseCucumberJSONReport(filePath string) ([]CucumberFeature, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cucumber json report file %s: %w", filePath, err)
	}

	if len(data) == 0 {
		// Empty file likely means no tests were run or an issue with output generation.
		// Return an empty slice of features and no error, as this might be a valid state (e.g., no tests selected).
		return []CucumberFeature{}, nil
	}

	var features []CucumberFeature
	err = json.Unmarshal(data, &features)
	if err != nil {
		// Attempt to unmarshal into a single feature object if the top level isn't an array
		// Some cucumber versions might output a single feature object if only one feature file is processed.
		var singleFeature CucumberFeature
		if singleErr := json.Unmarshal(data, &singleFeature); singleErr == nil {
			features = []CucumberFeature{singleFeature}
		} else {
			return nil, fmt.Errorf("failed to unmarshal cucumber json report from %s: %w. Single feature unmarshal error: %v", filePath, err, singleErr)
		}
	}
	return features, nil
}

// This is the function GetExamples will call.
// It's identical to ParseCucumberJSONReport for now, as the dry-run JSON structure
// for features and elements (scenarios) should be compatible.
// If dry-run JSON differs significantly for listing purposes, this function can be specialized.
func parseCucumberDryRunJSONOutput(filePath string) ([]CucumberFeature, error) {
	return ParseCucumberJSONReport(filePath)
}
