package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFetchFilesTiming(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
{
	"apple_spec.rb": 100,
	"banana_spec.rb": 300,
	"cherry_spec.rb": 200
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

	want := []fileTiming{
		{Path: "banana_spec.rb", Duration: 300},
		{Path: "cherry_spec.rb", Duration: 200},
		{Path: "apple_spec.rb", Duration: 100},
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
