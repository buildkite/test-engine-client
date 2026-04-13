package git

import (
	"context"
	"strings"
	"testing"
)

func TestFindForkPoint_Strategy1_ForkPoint(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"merge-base --fork-point origin/main abc123": "base111\n",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parent:     make(map[string]string),
	}

	result, err := FindForkPoint(context.Background(), runner, "origin/main", "abc123", mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Base != "base111" {
		t.Errorf("Base: got %q, want %q", result.Base, "base111")
	}
	if result.Strategy != "fork-point" {
		t.Errorf("Strategy: got %q, want %q", result.Strategy, "fork-point")
	}
}

func TestFindForkPoint_Strategy2_MainlineParent(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// Strategy 1 fails (no response for --fork-point)
		},
	}

	mc := &MainlineCache{
		onMainline: map[string]bool{"abc123": true},
		parent:     map[string]string{"abc123": "parent111"},
	}

	result, err := FindForkPoint(context.Background(), runner, "origin/main", "abc123", mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Base != "parent111" {
		t.Errorf("Base: got %q, want %q", result.Base, "parent111")
	}
	if result.Strategy != "parent-fallback" {
		t.Errorf("Strategy: got %q, want %q", result.Strategy, "parent-fallback")
	}
}

func TestFindForkPoint_Strategy3_MergeBase(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// Strategy 1 fails
			"merge-base origin/main abc123": "base222\n",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parent:     make(map[string]string),
	}

	result, err := FindForkPoint(context.Background(), runner, "origin/main", "abc123", mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Base != "base222" {
		t.Errorf("Base: got %q, want %q", result.Base, "base222")
	}
	if result.Strategy != "merge-base" {
		t.Errorf("Strategy: got %q, want %q", result.Strategy, "merge-base")
	}
}

func TestFindForkPoint_AllStrategiesFail(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parent:     make(map[string]string),
	}

	_, err := FindForkPoint(context.Background(), runner, "origin/main", "abc123", mc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "merge-base") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFindForkPoint_SkipsWhenForkPointEqualsSelf(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			// Strategy 1 returns the commit itself (useless)
			"merge-base --fork-point origin/main abc123": "abc123\n",
			// Falls through to strategy 3
			"merge-base origin/main abc123": "base333\n",
		},
	}

	mc := &MainlineCache{
		onMainline: make(map[string]bool),
		parent:     make(map[string]string),
	}

	result, err := FindForkPoint(context.Background(), runner, "origin/main", "abc123", mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Base != "base333" {
		t.Errorf("Base: got %q, want %q", result.Base, "base333")
	}
	if result.Strategy != "merge-base" {
		t.Errorf("Strategy: got %q, want %q", result.Strategy, "merge-base")
	}
}

func TestBuildMainlineCache(t *testing.T) {
	runner := &FakeGitRunner{
		Responses: map[string]string{
			"log --first-parent --since=90 days ago --format=%H %P origin/main": "aaa parent1\nbbb parent2\nccc\n",
		},
	}

	mc, err := BuildMainlineCache(context.Background(), runner, "origin/main", 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mc.Size() != 3 {
		t.Errorf("Size: got %d, want 3", mc.Size())
	}

	if !mc.onMainline["aaa"] {
		t.Error("expected aaa to be on mainline")
	}
	if !mc.onMainline["bbb"] {
		t.Error("expected bbb to be on mainline")
	}
	if !mc.onMainline["ccc"] {
		t.Error("expected ccc to be on mainline")
	}

	if mc.parent["aaa"] != "parent1" {
		t.Errorf("parent[aaa]: got %q, want %q", mc.parent["aaa"], "parent1")
	}
	if mc.parent["bbb"] != "parent2" {
		t.Errorf("parent[bbb]: got %q, want %q", mc.parent["bbb"], "parent2")
	}
	// ccc has no parent (initial commit)
	if _, ok := mc.parent["ccc"]; ok {
		t.Error("expected ccc to have no parent entry")
	}
}
