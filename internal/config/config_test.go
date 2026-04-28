package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	yaml := `
stages:
  - workspace: ./fetched
  - workspace: ./filtered
  - workspace: ./triaged

sources:
  - stage: fetched
    run: gh--current-pr-feedback
    format: json
    unwrap: inlineComments
    dedup_key: id

transitions:
  - from: fetched
    to: filtered
    condition: "not (isResolved == true or isOutdated == true)"
  - from: filtered
    to: triaged
    run: triage-pr-item
`

	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	p, baseDir, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if baseDir != dir {
		t.Errorf("baseDir = %q, want %q", baseDir, dir)
	}

	if len(p.Stages) != 3 {
		t.Fatalf("stages = %d, want 3", len(p.Stages))
	}
	if StageName(p.Stages[0]) != "fetched" {
		t.Errorf("stage 0 name = %q, want fetched", StageName(p.Stages[0]))
	}

	if len(p.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(p.Sources))
	}
	if p.Sources[0].DedupKey != "id" {
		t.Errorf("source dedup_key = %q, want id", p.Sources[0].DedupKey)
	}

	if len(p.Transitions) != 2 {
		t.Fatalf("transitions = %d, want 2", len(p.Transitions))
	}
	if p.Transitions[0].From != "fetched" {
		t.Errorf("transition 0 from = %q, want fetched", p.Transitions[0].From)
	}
}

func TestLoadNoStages(t *testing.T) {
	yaml := `
transitions:
  - from: a
    to: b
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing stages")
	}
}

func TestLoadUnknownStageInTransition(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
transitions:
  - from: a
    to: nonexistent
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown stage")
	}
}

func TestLoadSelfTransition(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
transitions:
  - from: a
    to: a
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for self-transition")
	}
}

func TestStageByName(t *testing.T) {
	p := &Pipeline{
		Stages: []Stage{
			{Workspace: "./foo"},
			{Workspace: "./bar"},
		},
	}

	s := p.StageByName("foo")
	if s == nil {
		t.Fatal("expected to find stage foo")
	}
	if s.Workspace != "./foo" {
		t.Errorf("workspace = %q, want ./foo", s.Workspace)
	}

	if p.StageByName("baz") != nil {
		t.Error("expected nil for nonexistent stage")
	}
}

func TestLoadBatchScope(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
  - workspace: ./b
transitions:
  - from: a
    to: b
    run: some-script
    scope: batch
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	p, _, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Transitions[0].Scope != "batch" {
		t.Errorf("scope = %q, want batch", p.Transitions[0].Scope)
	}
}

func TestLoadInvalidScope(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
  - workspace: ./b
transitions:
  - from: a
    to: b
    scope: grouped
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
}

func TestLoadSinkTransition(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
transitions:
  - from: a
    effect: sink
    run: post-comment
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	p, _, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Transitions[0].Effect != "sink" {
		t.Errorf("effect = %q, want sink", p.Transitions[0].Effect)
	}
}

func TestLoadSinkTransitionWithToFails(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
  - workspace: ./b
transitions:
  - from: a
    to: b
    effect: sink
    run: post-comment
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for sink transition with to")
	}
}

func TestLoadSinkTransitionRequiresRun(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
transitions:
  - from: a
    effect: sink
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for sink transition without run")
	}
}

func TestLoadInvalidEffect(t *testing.T) {
	yaml := `
stages:
  - workspace: ./a
  - workspace: ./b
transitions:
  - from: a
    to: b
    effect: stream
`
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid effect")
	}
}
