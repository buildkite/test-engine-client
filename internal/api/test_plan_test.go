package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func ptr[T any](t T) *T { return &t }

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

	got := FetchTestPlan(svr.URL, TestPlanParams{})
	want := TestPlan{
		Tasks: map[string]Task{
			"task_1": {
				NodeNumber: 1,
				Tests: Tests{
					Cases: []TestCase{{
						Path:              "dummy.spec",
						EstimatedDuration: ptr(1000000),
					}},
					Format: "junit",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchTestPlan diff (-got +want):\n%s", diff)
	}
}
