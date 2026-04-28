package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"flow/internal/config"
)

func TestValueAtPath(t *testing.T) {
	m := map[string]interface{}{
		"permalink": "top",
		"item": map[string]interface{}{
			"permalink": "nested",
		},
	}

	v, ok := valueAtPath(m, "permalink")
	if !ok || v != "top" {
		t.Fatalf("expected top permalink, got %v, ok=%v", v, ok)
	}

	v, ok = valueAtPath(m, "item.permalink")
	if !ok || v != "nested" {
		t.Fatalf("expected nested permalink, got %v, ok=%v", v, ok)
	}

	_, ok = valueAtPath(m, "item.missing")
	if ok {
		t.Fatalf("expected missing path to return ok=false")
	}
}

func TestEntityExistsInPipeline_MatchesNestedFromTopLevelKey(t *testing.T) {
	baseDir := t.TempDir()

	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./fetched"},
			{Workspace: "./qualified"},
		},
	}

	qualifiedEntities := filepath.Join(baseDir, "qualified", "entities")
	if err := os.MkdirAll(qualifiedEntities, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	entity := []byte("item:\n  permalink: https://example.test/comment/1\nis_trivial: true\ndone: false\n")
	if err := os.WriteFile(filepath.Join(qualifiedEntities, "e-1.yaml"), entity, 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}

	if !entityExistsInPipeline(p, baseDir, "permalink", "https://example.test/comment/1") {
		t.Fatalf("expected dedup check to match nested item.permalink")
	}
}

func TestEntityExistsInPipeline_MatchesExactNestedPath(t *testing.T) {
	baseDir := t.TempDir()

	p := &config.Pipeline{
		Stages: []config.Stage{{Workspace: "./qualified"}},
	}

	qualifiedEntities := filepath.Join(baseDir, "qualified", "entities")
	if err := os.MkdirAll(qualifiedEntities, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	entity := []byte("item:\n  permalink: https://example.test/comment/2\n")
	if err := os.WriteFile(filepath.Join(qualifiedEntities, "e-2.yaml"), entity, 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}

	if !entityExistsInPipeline(p, baseDir, "item.permalink", "https://example.test/comment/2") {
		t.Fatalf("expected dedup check to match explicit nested path")
	}
}

func TestConflictedSyncIDsInPipeline(t *testing.T) {
	baseDir := t.TempDir()

	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./a"},
			{Workspace: "./b"},
		},
	}

	for _, stage := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(baseDir, stage, "entities"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if err := os.WriteFile(filepath.Join(baseDir, "a", "entities", "e-1.yaml"), []byte("_sync:\n  id: thread-1\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "b", "entities", "e-2.yaml"), []byte("_sync:\n  id: thread-1\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "b", "entities", "e-3.yaml"), []byte("_sync:\n  id: thread-2\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}

	conflicted := conflictedSyncIDsInPipeline(p, baseDir)
	if !conflicted["thread-1"] {
		t.Fatalf("expected thread-1 to be conflicted")
	}
	if conflicted["thread-2"] {
		t.Fatalf("did not expect thread-2 to be conflicted")
	}
}

func TestFilterConflictedTransitionIDs(t *testing.T) {
	baseDir := t.TempDir()
	fromDir := filepath.Join(baseDir, "filtered")
	otherDir := filepath.Join(baseDir, "other")

	for _, dir := range []string{fromDir, otherDir} {
		if err := os.MkdirAll(filepath.Join(dir, "entities"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	p := &config.Pipeline{
		Stages: []config.Stage{
			{Workspace: "./filtered"},
			{Workspace: "./other"},
		},
	}

	if err := os.WriteFile(filepath.Join(fromDir, "entities", "a.yaml"), []byte("_sync:\n  id: thread-1\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fromDir, "entities", "b.yaml"), []byte("_sync:\n  id: thread-2\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "entities", "c.yaml"), []byte("_sync:\n  id: thread-1\n"), 0o644); err != nil {
		t.Fatalf("write entity: %v", err)
	}

	ids, rejected := filterConflictedTransitionIDs(p, baseDir, fromDir, []string{"a", "b"}, AdvanceOpts{})
	if rejected != 1 {
		t.Fatalf("rejected = %d, want 1", rejected)
	}
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("filtered ids = %v, want [b]", ids)
	}
}
