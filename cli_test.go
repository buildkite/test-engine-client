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

func TestCollectMetadataFlagIsGatedByPreviewEnv(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")
	if hasFlag(planCommandFlags(), "collect-metadata") {
		t.Fatalf("planCommandFlags() unexpectedly includes --collect-metadata when preview is disabled")
	}

	t.Setenv(previewSelectionEnvVar, "true")
	if !hasFlag(planCommandFlags(), "collect-metadata") {
		t.Fatalf("planCommandFlags() missing --collect-metadata when preview is enabled")
	}
}

func TestApplyPlanRequestContext_ClearsCollectMetadataWhenPreviewDisabled(t *testing.T) {
	t.Setenv(previewSelectionEnvVar, "")

	cfg.CollectMetadata = true
	cfg.SelectionStrategy = "percent"
	cfg.Metadata = map[string]string{"key": "val"}

	// Create a minimal command to satisfy the function signature.
	cmd := &cli.Command{}

	if err := applyPlanRequestContext(cmd); err != nil {
		t.Fatalf("applyPlanRequestContext() error = %v", err)
	}

	if cfg.CollectMetadata {
		t.Errorf("cfg.CollectMetadata = true, want false when preview is disabled")
	}
	if cfg.SelectionStrategy != "" {
		t.Errorf("cfg.SelectionStrategy = %q, want empty when preview is disabled", cfg.SelectionStrategy)
	}
	if cfg.Metadata != nil {
		t.Errorf("cfg.Metadata = %v, want nil when preview is disabled", cfg.Metadata)
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

func hasFlag(flags []cli.Flag, name string) bool {
	for _, flag := range flags {
		for _, n := range flag.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
}
