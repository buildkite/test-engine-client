package runner

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/test-splitter/internal/plan"
)

// In future, Rspec will implement an interface that defines
// behaviour common to all test runners.
// For now, Rspec provides rspec specific behaviour to execute
// and report on tests in the Rspec framework.
type Rspec struct {
}

// GetFiles returns an array of file names, for files in
// the "spec" directory that end in "spec.rb".
func (Rspec) GetFiles() ([]string, error) {
	var files []string

	// Use filepath.Walk to traverse the directory recursively
	err := filepath.Walk("spec", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(info.Name(), "_spec.rb") {
			files = append(files, path)
		}

		return nil
	})

	// Handle potential error from filepath.Walk
	if err != nil {
		return nil, err
	}

	return files, nil
}

// Command returns an exec.Cmd that will run the rspec command
func (Rspec) Command(testCases []string) *exec.Cmd {
	args := []string{"--options", ".rspec.ci"}

	args = append(args, testCases...)

	fmt.Println("+++ :test-analytics: Executing tests 🏃")
	fmt.Println("bin/rspec", strings.Join(args, " "))

	cmd := exec.Command("bin/rspec", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}

// RspecExample defines metadata for an rspec example (test).
type RspecExample struct {
	Id              string  `json:"id"`
	Description     string  `json:"description"`
	FullDescription string  `json:"full_description"`
	Status          string  `json:"status"`
	FilePath        string  `json:"file_path"`
	LineNumber      int     `json:"line_number"`
	RunTime         float64 `json:"run_time"`
}

// RspecReport defines metadata for an rspec generated test report.
type RspecReport struct {
	Version  string         `json:"version"`
	Seed     int            `json:"seed"`
	Examples []RspecExample `json:"examples"`
}

// Report produces a report that surfaces estimated and actual
// performance data for Rspec tests files.
//
// This is helpful for us in development, not sure if we will
// continue to maintain this functionality.
func (r Rspec) Report(w io.Writer, testCases []plan.TestCase) error {
	// get all rspec json files
	reportFiles, err := filepath.Glob("tmp/rspec-*.json")
	if err != nil {
		return fmt.Errorf("globbing for report files: %v", err)
	}

	// early return if no report files
	if len(reportFiles) == 0 {
		fmt.Fprintf(w, "No report files found")
		return nil
	}

	// read and parse all rspec json files
	var reports []RspecReport
	var errs []error
	for _, reportFile := range reportFiles {
		var report RspecReport
		if err := readJsonFile(reportFile, &report); err != nil {
			errs = append(errs, err)
			continue
		}
		reports = append(reports, report)
	}
	if len(errs) > 0 {
		return fmt.Errorf("reading report files: %v", errors.Join(errs...))
	}

	// aggregate execution time by file
	executionByFile := make(map[string]float64)
	for _, report := range reports {
		for _, example := range report.Examples {
			fileName := strings.Replace(example.FilePath, "./", "", 1)
			executionByFile[fileName] += example.RunTime
		}
	}

	// print report

	// calculate width for each column
	fileNameWidth := 0
	for _, testCase := range testCases {
		if len(testCase.Path) > fileNameWidth {
			fileNameWidth = len(testCase.Path)
		}
	}
	estimatedDurationWidth := len("Estimated")
	actualDurationWidth := len("Actual")
	predictionErrorWidth := len("Error %")

	// print header
	dashedLine := strings.Repeat("-", fileNameWidth+estimatedDurationWidth+actualDurationWidth+predictionErrorWidth+11) + "\n"
	fmt.Fprintf(w, dashedLine)
	fmt.Fprintf(w,
		"%-*s | %-*s | %-*s | %-*s |\n",
		fileNameWidth, "File Name",
		estimatedDurationWidth, "Estimated",
		actualDurationWidth, "Actual",
		predictionErrorWidth, "Error %",
	)
	fmt.Fprintf(w, dashedLine)

	// print each row
	for _, testCase := range testCases {

		var estimatedDuration time.Duration

		if testCase.EstimatedDuration != nil {
			estimatedDuration = time.Duration(*testCase.EstimatedDuration) * time.Microsecond
		}
		// Actual duration from rspec report is in second
		actualDuration := executionByFile[testCase.Path] * float64(time.Second)

		predictionError := math.Abs((actualDuration - float64(estimatedDuration)) / actualDuration * 100)

		fmt.Fprintf(w,
			"%-*s | %*s | %*s | %*s |\n",
			fileNameWidth, testCase.Path,
			estimatedDurationWidth, estimatedDuration.Truncate(time.Millisecond).String(),
			actualDurationWidth, time.Duration(actualDuration).Truncate(time.Millisecond).String(),
			predictionErrorWidth, fmt.Sprintf("%.2f%%", predictionError),
		)
	}
	fmt.Fprintf(w, dashedLine)

	return nil
}
