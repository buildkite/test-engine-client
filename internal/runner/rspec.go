package runner

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/test-splitter/internal/api"
)

type Rspec struct {
}

func (r Rspec) GetFiles() []string {
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
		log.Fatal("Error when getting files: ", err)
	}

	return files
}

func (r Rspec) Run(testCases []string) error {
	args := []string{"--options", ".rspec.ci"}

	args = append(args, testCases...)

	fmt.Println("+++ :test-analytics: Executing tests 🏃")
	fmt.Println("bin/rspec", strings.Join(args, " "))

	cmd := exec.Command("bin/rspec", args...)
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

type RspecExample struct {
	Id              string  `json:"id"`
	Description     string  `json:"description"`
	FullDescription string  `json:"full_description"`
	Status          string  `json:"status"`
	FilePath        string  `json:"file_path"`
	LineNumber      int     `json:"line_number"`
	RunTime         float64 `json:"run_time"`
}

type RspecReport struct {
	Version  string         `json:"version"`
	Seed     int            `json:"seed"`
	Examples []RspecExample `json:"examples"`
}

func (r Rspec) Report(testCases []api.TestCase) {
	// get all rspec json files
	reportFiles, err := filepath.Glob("tmp/rspec-*.json")
	if err != nil {
		fmt.Println("Error when getting report files: ", err)
	}

	// read and parse all rspec json files
	var reports []RspecReport
	for _, reportFile := range reportFiles {
		var report RspecReport

		err := readJsonFile(reportFile, &report)
		if err != nil {
			fmt.Println("Error when reading report file: ", err)
		}
		reports = append(reports, report)
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
	lines := strings.Repeat("-", fileNameWidth+estimatedDurationWidth+actualDurationWidth+predictionErrorWidth+11)
	fmt.Println(lines)
	fmt.Printf(
		"%-*s | %-*s | %-*s | %-*s |\n",
		fileNameWidth, "File Name",
		estimatedDurationWidth, "Estimated",
		actualDurationWidth, "Actual",
		predictionErrorWidth, "Error %",
	)
	fmt.Println(lines)

	// print each row
	for _, testCase := range testCases {
		estimatedDuration := *testCase.EstimatedDuration
		if err != nil {
			estimatedDuration = 0
		} else {
			// Estimated duration from API is an integer in microsecond
			estimatedDuration *= int(time.Microsecond)
		}

		// Actual duration from rspec report is in second
		actualDuration := executionByFile[testCase.Path] * float64(time.Second)

		predictionError := math.Abs((actualDuration - float64(estimatedDuration)) / actualDuration * 100)

		fmt.Printf(
			"%-*s | %*s | %*s | %*s |\n",
			fileNameWidth, testCase.Path,
			estimatedDurationWidth, time.Duration(estimatedDuration).Truncate(time.Millisecond).String(),
			actualDurationWidth, time.Duration(actualDuration).Truncate(time.Millisecond).String(),
			predictionErrorWidth, fmt.Sprintf("%.2f%%", predictionError),
		)
	}
	fmt.Println(lines)
}
