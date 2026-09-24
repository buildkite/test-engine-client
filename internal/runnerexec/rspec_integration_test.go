//go:build !windows && integration

package runnerexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

type rspecSource struct {
	testSource
	reports    []Result
	times      []time.Time
	dispatches []time.Time
}

func (s *rspecSource) Next() (*Batch, string, error) {
	s.dispatches = append(s.dispatches, time.Now())
	return s.testSource.Next()
}
func (s *rspecSource) Accepted(b Batch, r Result) {
	s.testSource.Accepted(b, r)
	s.reports = append(s.reports, r)
	s.times = append(s.times, time.Now())
}
func TestRSpecIntegration(t *testing.T) {
	root := os.Getenv("BKTEC_RSPEC_RUNNER_ROOT")
	if root == "" {
		t.Fatal("set BKTEC_RSPEC_RUNNER_ROOT to the test-collector-ruby package containing buildkite-rspec")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "spec"), 0700); err != nil {
		t.Fatal(err)
	}
	helper := `require 'buildkite/test_collector'
 require 'rspec/expectations'
 File.open('boots','a') { |f| f.puts Process.pid }
 Buildkite::TestCollector.configure(hook: :rspec)
 module Buildkite::TestCollector
   class Uploader
     def self.upload(data)
       File.open('records','a') { |f| f.puts JSON.generate(data.map(&:as_hash)) }
       nil
     end
   end
 end
 `
	spec := `RSpec.describe 'persistent' do
 it('flaky') { $attempt = ($attempt || 0)+1; File.open('executions','a') { |f| f.puts Process.pid }; expect($attempt).to be > 1 }
 it('passes') { File.open('executions','a') { |f| f.puts Process.pid }; expect(true).to eq(true) }
 end`
	if err := os.WriteFile(filepath.Join(dir, "spec/rails_helper.rb"), []byte(helper), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec/sample_spec.rb"), []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	s := &rspecSource{testSource: testSource{batches: []Batch{
		{ID: "initial", Tests: []plan.TestCase{{Format: "file", Path: "spec/sample_spec.rb"}}},
		{ID: "retry", Tests: []plan.TestCase{{Format: "example", Identifier: "./spec/sample_spec.rb[1:1]", Path: "ignored"}}},
		{ID: "later", Tests: []plan.TestCase{{Format: "selector", Value: "spec/sample_spec.rb"}}},
	}, done: "plan_completed"}}
	cmd := exec.Command("ruby", "-I", filepath.Join(root, "lib"), filepath.Join(root, "exe/buildkite-rspec"))
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	start := time.Now()
	err := Run(context.Background(), cmd, s, Options{StartupTimeout: 10 * time.Second, BatchTimeout: 10 * time.Second, ShutdownTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.reports) != 3 {
		t.Fatalf("reports %d", len(s.reports))
	}
	for i, r := range s.reports {
		var v struct {
			Summary struct {
				ExampleCount int `json:"example_count"`
				FailureCount int `json:"failure_count"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(r.Report, &v); err != nil {
			t.Fatal(err)
		}
		want := []int{2, 1, 2}[i]
		failures := []int{1, 0, 0}[i]
		if v.Summary.ExampleCount != want || v.Summary.FailureCount != failures {
			t.Fatalf("batch %d report %s", i, r.Report)
		}
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	boots, executions, records := read("boots"), read("executions"), read("records")
	if len(strings.Fields(string(boots))) != 1 || len(strings.Fields(string(executions))) != 5 {
		t.Fatalf("boots=%s executions=%s", boots, executions)
	}
	for _, pid := range strings.Fields(string(executions)) {
		if pid != strings.TrimSpace(string(boots)) {
			t.Fatal("PID changed")
		}
	}
	count := 0
	ids := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(records)), "\n") {
		var batch []map[string]any
		if err := json.Unmarshal([]byte(line), &batch); err != nil {
			t.Fatal(err)
		}
		for _, record := range batch {
			id := record["external_id"].(string)
			if ids[id] {
				t.Fatal("duplicate collector record")
			}
			ids[id] = true
			count++
		}
	}
	if count != 5 {
		t.Fatalf("collector records %d", count)
	}
	t.Logf("one boot; five same-PID executions; five distinct collector records")
	t.Logf("start through first pull=%s; batches=%s,%s,%s; total=%s", s.dispatches[0].Sub(start), s.times[0].Sub(s.dispatches[0]), s.times[1].Sub(s.dispatches[1]), s.times[2].Sub(s.dispatches[2]), time.Since(start))
	if fmt.Sprint(s.unresolved) != "[]" {
		t.Fatal(s.unresolved)
	}
}
