package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestFetchFilesTiming(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"apple_spec.rb": 1.1214749813079834,
	"banana_spec.rb": 3.1212387234762763,
	"cherry_spec.rb": 2.1436827355762394
}`)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	})

	files := []string{"apple_spec.rb", "banana_spec.rb", "cherry_spec.rb"}
	got, err := c.FetchFilesTiming("my-suite", files)
	if err != nil {
		t.Errorf("FetchFilesTiming() error = %v", err)
	}

	want := map[string]time.Duration{
		"apple_spec.rb":  1121474981,
		"banana_spec.rb": 3121238723,
		"cherry_spec.rb": 2143682735,
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("FetchFilesTiming() diff (-got +want):\n%s", diff)
	}
}

func TestFetchFilesTiming_Error(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "something went wrong"}`, http.StatusInternalServerError)
	}))
	defer svr.Close()

	c := NewClient(ClientConfig{
		OrganizationSlug: "my-org",
		ServerBaseUrl:    svr.URL,
	})

	files := []string{"apple_spec.rb", "banana_spec.rb"}
	_, err := c.FetchFilesTiming("my-suite", files)
	if err == nil {
		t.Errorf("FetchFilesTiming() error = %v, want an error", err)
	}

	want := "something went wrong"
	if got := err.Error(); got != want {
		t.Errorf("FetchFilesTiming() error = %v, want %v", got, want)
	}
}
