package env_test

import (
	"os"
	"testing"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/google/go-cmp/cmp"
)

// Note: out of the two implementations of interface env:
// - I'm testing env.OS because it's used by real code,
// - I'm not testing env.Map because it's only used in tests.

func TestOSGet(t *testing.T) {
	defer setenvWithUnset("BKTEC_ENV_TEST_VALUE", "hello")()

	env := env.OS{}

	got, want := env.Get("BKTEC_ENV_TEST_VALUE"), "hello"
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("env.Get() diff (-got +want):\n%s", diff)
	}
}

func TestOSGetMissing(t *testing.T) {
	os.Unsetenv("BKTEC_ENV_TEST_VALUE") // just in case

	env := env.OS{}

	got, want := env.Get("BKTEC_ENV_TEST_VALUE"), ""
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("env.Get() diff (-got +want):\n%s", diff)
	}
}

func TestOSLookup(t *testing.T) {
	defer setenvWithUnset("BKTEC_ENV_TEST_VALUE", "hello")()

	env := env.OS{}

	got, ok := env.Lookup("BKTEC_ENV_TEST_VALUE")
	want := "hello"

	if !ok {
		t.Errorf("env.Lookup() ok value should be true: %v", ok)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("env.Lookup() diff (-got +want):\n%s", diff)
	}
}

func TestOSLookupMissing(t *testing.T) {
	os.Unsetenv("BKTEC_ENV_TEST_VALUE") // just in case

	env := env.OS{}

	got, ok := env.Lookup("BKTEC_ENV_TEST_VALUE")
	want := ""

	if ok {
		t.Errorf("env.Lookup() ok value should be false: %v", ok)
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("env.Lookup() diff (-got +want):\n%s", diff)
	}
}

func TestOSDelete(t *testing.T) {
	defer setenvWithUnset("BKTEC_ENV_TEST_VALUE", "hello")()

	env := env.OS{}

	err := env.Delete("BKTEC_ENV_TEST_VALUE")
	if err != nil {
		t.Error(err)
	}

	got, want := os.Getenv("BKTEC_ENV_TEST_VALUE"), ""
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("os.Getenv() diff (-got +want):\n%s", diff)
	}
}

func TestOSSet(t *testing.T) {
	os.Unsetenv("BKTEC_ENV_TEST_VALUE")       // ensure pre-condition
	defer os.Unsetenv("BKTEC_ENV_TEST_VALUE") // ensure post-condition (cleanup)

	env := env.OS{}

	err := env.Set("BKTEC_ENV_TEST_VALUE", "Set()")
	if err != nil {
		t.Error(err)
	}

	got, want := os.Getenv("BKTEC_ENV_TEST_VALUE"), "Set()"
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("os.Getenv() diff (-got +want):\n%s", diff)
	}
}

// intended to be called like: `defer setenvWithUnset(...)()`
func setenvWithUnset(key string, value string) func() {
	os.Setenv(key, value)
	return func() { os.Unsetenv(key) }
}
