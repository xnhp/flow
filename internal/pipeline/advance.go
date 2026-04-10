package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"flow/internal/config"
	"flow/internal/sap"

	"gopkg.in/yaml.v3"
)

// AdvanceOpts controls the behaviour of Advance.
type AdvanceOpts struct {
	NoSources bool
	Verbose   bool
}

// Advance evaluates all sources and transitions in DAG order, moving eligible entities.
func Advance(p *config.Pipeline, baseDir string, opts AdvanceOpts) error {
	// Ensure all stage workspaces exist.
	if err := ensureWorkspaces(p, baseDir); err != nil {
		return fmt.Errorf("ensure workspaces: %w", err)
	}

	// 1. Source fetching (unless --no-sources).
	if !opts.NoSources {
		for _, src := range p.Sources {
			if err := runSource(p, baseDir, src, opts); err != nil {
				log.Printf("source %q: %v", src.Stage, err)
			}
		}
	}

	// 2. Evaluate transitions in topological order.
	order, err := TopoOrder(p)
	if err != nil {
		return fmt.Errorf("compute transition order: %w", err)
	}

	for _, idx := range order {
		t := p.Transitions[idx]
		if err := runTransition(p, baseDir, t, opts); err != nil {
			log.Printf("transition %s->%s: %v", t.From, t.To, err)
		}
	}

	return nil
}

func ensureWorkspaces(p *config.Pipeline, baseDir string) error {
	for _, stage := range p.Stages {
		dir := resolvePath(baseDir, stage.Workspace)
		if sap.WorkspaceExists(dir) {
			continue
		}
		// Create the directory if needed.
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		// Init with schema if specified.
		schemaPath := stage.Schema
		if schemaPath == "" {
			// No schema specified; just create the directory.
			// The workspace won't be fully functional until a schema is provided,
			// but that's OK for stages that might get their schema from elsewhere.
			continue
		}
		schemaPath = resolvePath(baseDir, schemaPath)
		// Copy sibling schema files first so $ref targets resolve during init.
		if err := copySiblingSchemas(schemaPath, dir); err != nil {
			return fmt.Errorf("copy sibling schemas for %s: %w", dir, err)
		}
		if err := sap.WorkspaceInit(dir, schemaPath); err != nil {
			return fmt.Errorf("init workspace %s: %w", dir, err)
		}
	}
	return nil
}

// copySiblingSchemas copies *.schema.yaml files from the source schema's
// directory into the workspace directory so that $ref targets resolve.
func copySiblingSchemas(schemaPath, workspaceDir string) error {
	srcDir := filepath.Dir(schemaPath)
	srcBase := filepath.Base(schemaPath)

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == srcBase {
			continue
		}
		if !strings.HasSuffix(name, ".schema.yaml") && !strings.HasSuffix(name, ".schema.json") {
			continue
		}
		dst := filepath.Join(workspaceDir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // already exists
		}
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func runSource(p *config.Pipeline, baseDir string, src config.Source, opts AdvanceOpts) error {
	stageDir := resolveStageDir(p, baseDir, src.Stage)
	if stageDir == "" {
		return fmt.Errorf("stage %q not found", src.Stage)
	}

	// Run the fetch executable.
	execPath, err := resolveExecutable(src.Run)
	if err != nil {
		return fmt.Errorf("resolve executable %q: %w", src.Run, err)
	}

	var args []string
	for k, v := range src.Args {
		if v == "true" {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k, v)
		}
	}

	cmd := exec.Command(execPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Verbose {
		log.Printf("source %s: running %s %v", src.Stage, execPath, args)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %s: %w", execPath, stderr.String(), err)
	}

	// Parse output.
	output := stdout.Bytes()
	items, err := parseSourceOutput(output, src.Format, src.Unwrap)
	if err != nil {
		return fmt.Errorf("parse source output: %w", err)
	}

	if opts.Verbose {
		log.Printf("source %s: fetched %d items", src.Stage, len(items))
	}

	// Dedup and import.
	var importedIDs []string
	for _, item := range items {
		if src.DedupKey != "" {
			key := item[src.DedupKey]
			if key != nil && entityExistsInPipeline(p, baseDir, src.DedupKey, key) {
				continue
			}
		}

		yamlData, err := yaml.Marshal(item)
		if err != nil {
			log.Printf("source %s: marshal entity: %v", src.Stage, err)
			continue
		}

		id, err := sap.Import(stageDir, yamlData)
		if err != nil {
			log.Printf("source %s: import entity: %v", src.Stage, err)
			continue
		}
		importedIDs = append(importedIDs, extractImportedID(id))
	}

	if opts.Verbose {
		log.Printf("source %s: imported %d new entities", src.Stage, len(importedIDs))
	}

	// Trigger post-import hooks.
	if len(importedIDs) > 0 {
		if err := sap.HooksRun(stageDir, "post-import", importedIDs); err != nil {
			log.Printf("source %s: hooks: %v", src.Stage, err)
		}
	}

	return nil
}

func runTransition(p *config.Pipeline, baseDir string, t config.Transition, opts AdvanceOpts) error {
	fromDir := resolveStageDir(p, baseDir, t.From)
	toDir := resolveStageDir(p, baseDir, t.To)

	if fromDir == "" || toDir == "" {
		return fmt.Errorf("stages not found: from=%q to=%q", t.From, t.To)
	}

	// Find eligible entities.
	var ids []string
	var err error
	if t.Condition != "" {
		ids, err = sap.Query(fromDir, t.Condition)
	} else {
		ids, err = sap.AllEntityIDs(fromDir)
	}
	if err != nil {
		return fmt.Errorf("query eligible entities: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	if opts.Verbose {
		log.Printf("transition %s->%s: %d eligible entities", t.From, t.To, len(ids))
	}

	scope := t.Scope
	if scope == "" {
		scope = "entity"
	}

	switch scope {
	case "entity":
		return runEntityTransition(fromDir, toDir, t, ids, opts)
	case "batch":
		return runBatchTransition(fromDir, toDir, t, ids, opts)
	default:
		return fmt.Errorf("unknown scope %q", scope)
	}
}

func runEntityTransition(fromDir, toDir string, t config.Transition, ids []string, opts AdvanceOpts) error {
	type result struct {
		id         string
		importedID string
		err        error
	}

	results := make(chan result, len(ids))
	var wg sync.WaitGroup

	for _, id := range ids {
		wg.Add(1)
		go func(entityID string) {
			defer wg.Done()

			entityData, err := sap.ReadEntity(fromDir, entityID)
			if err != nil {
				results <- result{entityID, "", fmt.Errorf("read entity: %w", err)}
				return
			}

			var outputData []byte
			if t.Run != "" {
				outputData, err = runAction(t.Run, t.Args, entityData, opts)
				if err != nil {
					results <- result{entityID, "", fmt.Errorf("run action: %w", err)}
					return
				}
			} else {
				// No executable -- pure filter/move.
				outputData = entityData
			}

			importOut, err := sap.Import(toDir, outputData)
			if err != nil {
				results <- result{entityID, "", fmt.Errorf("import to %s: %w", toDir, err)}
				return
			}

			if err := sap.Remove(fromDir, entityID); err != nil {
				results <- result{entityID, "", fmt.Errorf("remove from %s: %w", fromDir, err)}
				return
			}

			results <- result{entityID, extractImportedID(importOut), nil}
		}(id)
	}

	wg.Wait()
	close(results)

	var errs []string
	var importedIDs []string
	for r := range results {
		if r.err != nil {
			log.Printf("  entity %s: %v", r.id, r.err)
			errs = append(errs, r.id)
		} else {
			importedIDs = append(importedIDs, r.importedID)
		}
	}

	if opts.Verbose {
		log.Printf("transition %s->%s: %d succeeded, %d failed", t.From, t.To, len(importedIDs), len(errs))
	}

	// Trigger post-import hooks on destination workspace.
	if len(importedIDs) > 0 {
		if err := sap.HooksRun(toDir, "post-import", importedIDs); err != nil {
			log.Printf("transition %s->%s: hooks: %v", t.From, t.To, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d entities failed", len(errs))
	}
	return nil
}

func runBatchTransition(fromDir, toDir string, t config.Transition, ids []string, opts AdvanceOpts) error {
	// Read all entities from YAML files.
	var entities []map[string]interface{}
	for _, id := range ids {
		data, err := os.ReadFile(filepath.Join(fromDir, "entities", id+".yaml"))
		if err != nil {
			return fmt.Errorf("read entity %s: %w", id, err)
		}
		var m map[string]interface{}
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parse entity %s: %w", id, err)
		}
		entities = append(entities, m)
	}

	input, err := json.Marshal(entities)
	if err != nil {
		return fmt.Errorf("marshal batch input: %w", err)
	}

	if t.Run == "" {
		// No executable -- just move all entities.
		var importedIDs []string
		for _, id := range ids {
			data, _ := sap.ReadEntity(fromDir, id)
			importOut, err := sap.Import(toDir, data)
			if err != nil {
				log.Printf("  batch move %s: import: %v", id, err)
				continue
			}
			importedIDs = append(importedIDs, extractImportedID(importOut))
			sap.Remove(fromDir, id)
		}
		if len(importedIDs) > 0 {
			if err := sap.HooksRun(toDir, "post-import", importedIDs); err != nil {
				log.Printf("  batch move: hooks: %v", err)
			}
		}
		return nil
	}

	outputData, err := runAction(t.Run, t.Args, input, opts)
	if err != nil {
		return fmt.Errorf("batch action: %w", err)
	}

	// Parse output as array of entities.
	var outputEntities []map[string]interface{}
	if err := json.Unmarshal(outputData, &outputEntities); err != nil {
		return fmt.Errorf("parse batch output: %w", err)
	}

	// Import all output entities.
	var importedIDs []string
	for _, ent := range outputEntities {
		yamlData, err := yaml.Marshal(ent)
		if err != nil {
			log.Printf("  batch: marshal output entity: %v", err)
			continue
		}
		importOut, err := sap.Import(toDir, yamlData)
		if err != nil {
			log.Printf("  batch: import output entity: %v", err)
			continue
		}
		importedIDs = append(importedIDs, extractImportedID(importOut))
	}

	// Remove all source entities.
	for _, id := range ids {
		sap.Remove(fromDir, id)
	}

	// Trigger post-import hooks on destination workspace.
	if len(importedIDs) > 0 {
		if err := sap.HooksRun(toDir, "post-import", importedIDs); err != nil {
			log.Printf("  batch: hooks: %v", err)
		}
	}

	return nil
}

func runAction(executable string, args map[string]string, stdin []byte, opts AdvanceOpts) ([]byte, error) {
	execPath, err := resolveExecutable(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve executable %q: %w", executable, err)
	}

	var cmdArgs []string
	for k, v := range args {
		if v == "true" {
			cmdArgs = append(cmdArgs, "--"+k)
		} else {
			cmdArgs = append(cmdArgs, "--"+k, v)
		}
	}

	cmd := exec.Command(execPath, cmdArgs...)
	cmd.Stdin = bytes.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if opts.Verbose {
		log.Printf("  action: %s %v", execPath, cmdArgs)
	}

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", execPath, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

// extractImportedID extracts the entity ID from sap import output like "import ok: e-000001".
func extractImportedID(output string) string {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "import ok: ") {
		return strings.TrimPrefix(output, "import ok: ")
	}
	return output
}

// parseSourceOutput parses the output of a source executable.
func parseSourceOutput(data []byte, format, unwrap string) ([]map[string]interface{}, error) {
	if format == "" {
		format = "json"
	}

	switch format {
	case "json":
		return parseJSONOutput(data, unwrap)
	case "yaml":
		return parseYAMLOutput(data, unwrap)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func parseJSONOutput(data []byte, unwrap string) ([]map[string]interface{}, error) {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if unwrap != "" {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object for unwrap, got %T", raw)
		}
		raw = m[unwrap]
	}

	return toEntityList(raw)
}

func parseYAMLOutput(data []byte, unwrap string) ([]map[string]interface{}, error) {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	if unwrap != "" {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object for unwrap, got %T", raw)
		}
		raw = m[unwrap]
	}

	return toEntityList(raw)
}

func toEntityList(raw interface{}) ([]map[string]interface{}, error) {
	switch v := raw.(type) {
	case []interface{}:
		var result []map[string]interface{}
		for i, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("item %d: expected object, got %T", i, item)
			}
			normalizeFloats(m)
			result = append(result, m)
		}
		return result, nil
	case map[string]interface{}:
		normalizeFloats(v)
		return []map[string]interface{}{v}, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected array or object, got %T", raw)
	}
}

// normalizeFloats converts JSON float64 values that are whole numbers to int64
// so they serialize cleanly in YAML (e.g. 3046863006 instead of 3.046863006e+09).
func normalizeFloats(m map[string]interface{}) {
	for k, v := range m {
		switch val := v.(type) {
		case float64:
			if val == float64(int64(val)) {
				m[k] = int64(val)
			}
		case map[string]interface{}:
			normalizeFloats(val)
		case []interface{}:
			for i, item := range val {
				if sub, ok := item.(map[string]interface{}); ok {
					normalizeFloats(sub)
				} else if f, ok := item.(float64); ok && f == float64(int64(f)) {
					val[i] = int64(f)
				}
			}
		}
	}
}

// entityExistsInPipeline checks all stages for an entity with the given property value
// by reading entity YAML files directly.
func entityExistsInPipeline(p *config.Pipeline, baseDir string, property string, value interface{}) bool {
	needle := fmt.Sprintf("%v", value)
	for _, stage := range p.Stages {
		dir := resolvePath(baseDir, stage.Workspace)
		entDir := filepath.Join(dir, "entities")
		entries, err := os.ReadDir(entDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(entDir, e.Name()))
			if err != nil {
				continue
			}
			var m map[string]interface{}
			if err := yaml.Unmarshal(data, &m); err != nil {
				continue
			}
			if fmt.Sprintf("%v", m[property]) == needle {
				return true
			}
		}
	}
	return false
}

func resolveStageDir(p *config.Pipeline, baseDir string, stageName string) string {
	stage := p.StageByName(stageName)
	if stage == nil {
		return ""
	}
	return resolvePath(baseDir, stage.Workspace)
}

func resolvePath(baseDir, path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func resolveExecutable(name string) (string, error) {
	// Tilde expansion.
	if strings.HasPrefix(name, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, name[2:]), nil
	}

	// Absolute or relative path with slashes -- use as-is.
	if strings.Contains(name, "/") {
		return name, nil
	}

	// Look up in PATH.
	p, err := exec.LookPath(name)
	if err == nil {
		return p, nil
	}

	// Fallback: ~/.config/flow/scripts/
	home, _ := os.UserHomeDir()
	candidate := filepath.Join(home, ".config", "flow", "scripts", name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("executable %q not found in PATH or ~/.config/flow/scripts/", name)
}
