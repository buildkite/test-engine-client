package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/test-splitter/internal/config"
	"github.com/buildkite/test-splitter/internal/plan"
	"github.com/google/go-cmp/cmp"
)

func TestFetchOrCreateTestPlan(t *testing.T) {
	files := []string{"apple"}

	// mock server to return a test plan
	response := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": {
				"cases": [
					{
						"path": "apple"
					}
				],
				"format": "files"
			}
		}
	}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, response)
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   10,
		SuiteToken:    "suite_token",
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return the test plan fetched from the server
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests: plan.Tests{
					Cases:  []plan.TestCase{{Path: "apple"}},
					Format: "files",
				},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(ctx, cfg, files)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, files, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}

func TestFetchOrCreateTestPlan_PlanError(t *testing.T) {
	files := []string{"apple", "banana", "cherry", "mango"}
	tests := plan.Tests{
		Cases:  []plan.TestCase{{Path: "apple"}, {Path: "banana"}, {Path: "cherry"}, {Path: "mango"}},
		Format: "files",
	}

	// mock server to return an error plan
	response := `{
	"tasks": {}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, response)
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		SuiteToken:    "suite_token",
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(tests, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, cfg, files)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_InternalServerError(t *testing.T) {
	files := []string{"red", "orange", "yellow", "green", "blue", "indigo", "violet"}
	tests := plan.Tests{
		Cases:  []plan.TestCase{{Path: "red"}, {Path: "orange"}, {Path: "yellow"}, {Path: "green"}, {Path: "blue"}, {Path: "indigo"}, {Path: "violet"}},
		Format: "files",
	}

	// mock server to return a 500 Internal Server Error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   3,
		SuiteToken:    "suite_token",
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(tests, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(ctx, cfg, files)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_BadRequest(t *testing.T) {
	files := []string{"apple", "banana"}
	tests := plan.Tests{
		Cases:  []plan.TestCase{{Path: "apple"}, {Path: "banana"}},
		Format: "files",
	}

	// mock server to return 400 Bad Request
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer svr.Close()

	ctx := context.Background()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		SuiteToken:    "suite_token",
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return an empty test plan and an error
	want := plan.TestPlan{}

	got, err := fetchOrCreateTestPlan(ctx, cfg, files)
	if err == nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) want error, got %v", cfg, tests, err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}
