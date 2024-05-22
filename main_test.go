package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
			"tests": [
				{
					"path": "apple",
					"format": "file"
				}
			]
		}
	}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// simulate cache miss for GET test_plan so it will trigger the test plan creation
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
		} else {
			fmt.Fprint(w, response)
		}
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   10,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return the test plan fetched from the server
	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
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

func TestFetchOrCreateTestPlan_CachedPlan(t *testing.T) {
	cachedPlan := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": [
				{
					"path": "apple",
					"format": "file"
				}
			]
		}
	}
}`

	newPlan := `{
	"tasks": {
		"0": {
			"node_number": 0,
			"tests": [
				{
					"path": "banana",
					"format": "file"
				}
			]
		}
	}
}`

	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, cachedPlan)
		} else {
			fmt.Fprint(w, newPlan)
		}
	}))
	defer svr.Close()

	cfg := config.Config{
		NodeIndex:        0,
		Parallelism:      10,
		Identifier:       "identifier",
		ServerBaseUrl:    svr.URL,
		OrganizationSlug: "org",
		SuiteSlug:        "suite",
	}

	tests := []string{"banana"}

	want := plan.TestPlan{
		Tasks: map[string]*plan.Task{
			"0": {
				NodeNumber: 0,
				Tests:      []plan.TestCase{{Path: "apple", Format: plan.TestCaseFormatFile}},
			},
		},
	}

	got, err := fetchOrCreateTestPlan(context.Background(), cfg, tests)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_PlanError(t *testing.T) {
	files := []string{"apple", "banana", "cherry", "mango"}
	tests := []plan.TestCase{
		{Path: "apple", Format: plan.TestCaseFormatFile},
		{Path: "banana", Format: plan.TestCaseFormatFile},
		{Path: "cherry", Format: plan.TestCaseFormatFile},
		{Path: "mango", Format: plan.TestCaseFormatFile},
	}

	// mock server to return an error plan
	response := `{
	"tasks": {}
}`
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// simulate cache miss for GET test_plan so it will trigger the test plan creation
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
		}
		fmt.Fprint(w, response)
	}))
	defer svr.Close()

	ctx := context.Background()
	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
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
	tests := []plan.TestCase{
		{Path: "red", Format: plan.TestCaseFormatFile},
		{Path: "orange", Format: plan.TestCaseFormatFile},
		{Path: "yellow", Format: plan.TestCaseFormatFile},
		{Path: "green", Format: plan.TestCaseFormatFile},
		{Path: "blue", Format: plan.TestCaseFormatFile},
		{Path: "indigo", Format: plan.TestCaseFormatFile},
		{Path: "violet", Format: plan.TestCaseFormatFile},
	}

	// mock server to return a 500 Internal Server Error
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer svr.Close()

	// set the fetch timeout to 1 second so we don't wait too long
	ctx := context.Background()
	fetchCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   3,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return a fallback plan
	want := plan.CreateFallbackPlan(tests, cfg.Parallelism)

	got, err := fetchOrCreateTestPlan(fetchCtx, cfg, files)
	if err != nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) error = %v", cfg, tests, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, tests, diff)
	}
}

func TestFetchOrCreateTestPlan_BadRequest(t *testing.T) {
	files := []string{"apple", "banana"}

	// mock server to return 400 Bad Request
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer svr.Close()

	ctx := context.Background()

	cfg := config.Config{
		NodeIndex:     0,
		Parallelism:   2,
		Identifier:    "identifier",
		ServerBaseUrl: svr.URL,
	}

	// we want the function to return an empty test plan and an error
	want := plan.TestPlan{}

	got, err := fetchOrCreateTestPlan(ctx, cfg, files)
	if err == nil {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) want error, got %v", cfg, files, err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("fetchOrCreateTestPlan(ctx, %v, %v) diff (-got +want):\n%s", cfg, files, diff)
	}
}
