package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func ptr[T any](t T) *T { return &t }

// Referencing this post to test program exit with status 1 https://blog.antoine-augusti.fr/2015/12/testing-an-os-exit-scenario-in-golang/
func TestFetchTestPlan_Error4xx(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer svr.Close()

	// Only run the failing part when a specific env variable is set
	if os.Getenv("4XX_ERROR") == "1" {
		params := TestPlanParams{}
		FetchTestPlan(svr.URL, params)
		return
	}

	// Start the actual test in a different subprocess
	cmd := exec.Command(os.Args[0], "-test.run=TestFetchTestPlan_Error4xx")
	stdout, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Check that the log fatal message is what we expected
	gotBytes, _ := io.ReadAll(stdout)
	got := string(gotBytes)
	expected := "Cannot process the request"
	if !strings.HasSuffix(got[:len(got)-1], expected) {
		t.Fatalf("Unexpected log message. Got %s but should contain %s", got[:len(got)-1], expected)
	}

	// Check that the program exited
	err := cmd.Wait()
	if e, ok := err.(*exec.ExitError); !ok || e.Success() {
		t.Fatalf("Process ran with err %v, want exit status 1", err)
	}
}

func TestFetchTestPlan(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
	"tasks": {
		"task_1": {
			"node_number": 1,
			"tests": {
				"cases": [
					{
						"path": "dummy.spec",
						"estimated_duration": 1000000
					}
				],
				"format": "junit"
			}
		}
	}
}`)
	}))
	defer svr.Close()

	params := TestPlanParams{}
	got, err := FetchTestPlan(svr.URL, params)
	if err != nil {
		t.Errorf("FetchTestPlan(%q, %v) error = %v", svr.URL, params, err)
	}
	want := TestPlan{
		Tasks: map[string]Task{
			"task_1": {
				NodeNumber: 1,
				Tests: Tests{
					Cases: []TestCase{{
						Path:              "dummy.spec",
						EstimatedDuration: ptr(1_000_000),
					}},
					Format: "junit",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchTestPlan(%q, %v) diff (-got +want):\n%s", svr.URL, params, diff)
	}
}
