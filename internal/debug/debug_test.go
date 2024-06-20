package debug

import (
	"bytes"
	"regexp"
	"testing"
)

func TestPrintf(t *testing.T) {
	var output bytes.Buffer

	SetDebug(true)
	SetOutput(&output)

	Printf("Hello, %s!", "world")

	want := "DEBUG: Hello, world!\n"
	matched, err := regexp.MatchString(want, output.String())
	if err != nil {
		t.Errorf("error matching output: %v", err)
	}

	if !matched {
		t.Errorf("output doesn't match: got %q, want %q", output.String(), want)
	}
}

func TestPrintf_disabled(t *testing.T) {
	var output bytes.Buffer

	SetDebug(false)
	SetOutput(&output)

	Printf("Hello, %s!", "world")
	if output.String() != "" {
		t.Errorf("output should be empty, got %q", output.String())
	}
}

func TestPrintln(t *testing.T) {
	var output bytes.Buffer

	SetDebug(true)
	SetOutput(&output)

	Println("Hello world!")

	want := "DEBUG: Hello world!\n"
	matched, err := regexp.MatchString(want, output.String())
	if err != nil {
		t.Errorf("error matching output: %v", err)
	}

	if !matched {
		t.Errorf("output doesn't match: got %q, want %q", output.String(), want)
	}
}

func TestPrintln_disabled(t *testing.T) {
	var output bytes.Buffer

	SetDebug(false)
	SetOutput(&output)

	Println("Hello world!")
	if output.String() != "" {
		t.Errorf("output should be empty, got %q", output.String())
	}
}
