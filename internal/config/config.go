// Package config handles loading and validating flow pipeline configurations.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Pipeline is the top-level flow.yaml structure.
type Pipeline struct {
	Stages      []Stage      `yaml:"stages"`
	Sources     []Source     `yaml:"sources"`
	Transitions []Transition `yaml:"transitions"`
}

// Stage declares a sap workspace in the pipeline.
type Stage struct {
	Workspace string `yaml:"workspace"`
	Schema    string `yaml:"schema"`
}

// Source declares an external fetch that populates a stage.
type Source struct {
	Stage    string            `yaml:"stage"`
	Run      string            `yaml:"run"`
	Args     map[string]string `yaml:"args,omitempty"`
	Format   string            `yaml:"format,omitempty"` // json (default) or yaml
	Unwrap   string            `yaml:"unwrap,omitempty"` // property to unwrap from output
	DedupKey string            `yaml:"dedup_key,omitempty"`
}

// Transition declares movement of entities between stages.
type Transition struct {
	From         string            `yaml:"from"`
	To           string            `yaml:"to"`
	Condition    string            `yaml:"condition,omitempty"`
	CriteriaRun  string            `yaml:"criteria_run,omitempty"`
	CriteriaArgs map[string]string `yaml:"criteria_args,omitempty"`
	Run          string            `yaml:"run,omitempty"`
	Args         map[string]string `yaml:"args,omitempty"`
	Scope        string            `yaml:"scope,omitempty"` // "entity" (default) or "batch"
}

// Load reads a flow.yaml file and returns a validated Pipeline.
func Load(path string) (*Pipeline, string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, "", fmt.Errorf("read config: %w", err)
	}

	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, "", fmt.Errorf("parse config: %w", err)
	}

	baseDir := filepath.Dir(absPath)

	if err := validate(&p); err != nil {
		return nil, "", err
	}

	return &p, baseDir, nil
}

func validate(p *Pipeline) error {
	if len(p.Stages) == 0 {
		return fmt.Errorf("pipeline must have at least one stage")
	}

	stageNames := make(map[string]bool, len(p.Stages))
	for i, s := range p.Stages {
		if s.Workspace == "" {
			return fmt.Errorf("stage %d: workspace path is required", i)
		}
		// Use workspace basename as the stage name for referencing.
		name := filepath.Base(s.Workspace)
		if stageNames[name] {
			return fmt.Errorf("stage %d: duplicate stage name %q (from workspace path)", i, name)
		}
		stageNames[name] = true
	}

	for i, src := range p.Sources {
		if src.Stage == "" {
			return fmt.Errorf("source %d: stage is required", i)
		}
		if !stageNames[src.Stage] {
			return fmt.Errorf("source %d: unknown stage %q", i, src.Stage)
		}
		if src.Run == "" {
			return fmt.Errorf("source %d: run is required", i)
		}
	}

	for i, t := range p.Transitions {
		if t.From == "" {
			return fmt.Errorf("transition %d: from is required", i)
		}
		if t.To == "" {
			return fmt.Errorf("transition %d: to is required", i)
		}
		if !stageNames[t.From] {
			return fmt.Errorf("transition %d: unknown source stage %q", i, t.From)
		}
		if !stageNames[t.To] {
			return fmt.Errorf("transition %d: unknown destination stage %q", i, t.To)
		}
		if t.From == t.To {
			return fmt.Errorf("transition %d: from and to cannot be the same stage %q", i, t.From)
		}
		if t.Scope != "" && t.Scope != "entity" && t.Scope != "batch" {
			return fmt.Errorf("transition %d: scope must be 'entity' or 'batch', got %q", i, t.Scope)
		}
		if t.CriteriaRun == "" && len(t.CriteriaArgs) > 0 {
			return fmt.Errorf("transition %d: criteria_args requires criteria_run", i)
		}
	}

	return nil
}

// StageName returns the short name for a stage (basename of workspace path).
func StageName(s Stage) string {
	return filepath.Base(s.Workspace)
}

// StageByName finds a stage by its short name.
func (p *Pipeline) StageByName(name string) *Stage {
	for i := range p.Stages {
		if StageName(p.Stages[i]) == name {
			return &p.Stages[i]
		}
	}
	return nil
}
