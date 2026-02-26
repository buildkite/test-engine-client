package main

import (
	"testing"

	"github.com/urfave/cli/v3"
)

func TestPreviewSelectionEnabled(t *testing.T) {
	truthyValues := []string{"1", "true", "TRUE", "yes", "on", "t", "y"}
	for _, value := range truthyValues {
		t.Run(value, func(t *testing.T) {
			t.Setenv(previewSelectionEnvVar, value)
			if !previewSelectionEnabled() {
				t.Fatalf("previewSelectionEnabled() = false, want true for %q", value)
			}
		})
	}

	falsyValues := []string{"", "0", "false", "off", "no", "random"}
	for _, value := range falsyValues {
		t.Run(value, func(t *testing.T) {
			t.Setenv(previewSelectionEnvVar, value)
			if previewSelectionEnabled() {
				t.Fatalf("previewSelectionEnabled() = true, want false for %q", value)
			}
		})
	}
}

func TestSelectionFlagsAreGatedByPreviewEnv(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")
	if hasSelectionFlag(runCommandFlags()) {
		t.Fatalf("runCommandFlags() unexpectedly includes selection flags when preview is disabled")
	}
	if hasSelectionFlag(planCommandFlags()) {
		t.Fatalf("planCommandFlags() unexpectedly includes selection flags when preview is disabled")
	}

	t.Setenv(previewSelectionEnvVar, "true")
	if !hasSelectionFlag(runCommandFlags()) {
		t.Fatalf("runCommandFlags() missing selection flags when preview is enabled")
	}
	if !hasSelectionFlag(planCommandFlags()) {
		t.Fatalf("planCommandFlags() missing selection flags when preview is enabled")
	}
}

func hasSelectionFlag(flags []cli.Flag) bool {
	for _, flag := range flags {
		for _, name := range flag.Names() {
			if name == "selection-strategy" || name == "selection-param" || name == "metadata" {
				return true
			}
		}
	}

	return false
}
