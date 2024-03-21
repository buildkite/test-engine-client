package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	fmt.Println("bin/rspec", strings.Join(args, " "))

	cmd := exec.Command("bin/rspec", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}
