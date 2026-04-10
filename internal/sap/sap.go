// Package sap provides functions to invoke sap CLI commands.
package sap

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Query runs `sap query` in the given workspace directory and returns matching entity IDs.
func Query(workspaceDir string, condition string) ([]string, error) {
	args := []string{"query", condition, "--format", "ids"}
	cmd := exec.Command("sap", args...)
	cmd.Dir = workspaceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sap query in %s: %s: %w", workspaceDir, stderr.String(), err)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// AllEntityIDs returns all entity IDs in the workspace by listing the entities directory.
func AllEntityIDs(workspaceDir string) ([]string, error) {
	entDir := filepath.Join(workspaceDir, "entities")
	entries, err := os.ReadDir(entDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read entities dir %s: %w", entDir, err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	return ids, nil
}

// ReadEntity runs `sap read <id>` in the given workspace and returns the YAML content.
func ReadEntity(workspaceDir string, id string) ([]byte, error) {
	cmd := exec.Command("sap", "read", id)
	cmd.Dir = workspaceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sap read %s in %s: %s: %w", id, workspaceDir, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

// Import runs `sap import <file>` in the given workspace directory.
// Returns the ID of the imported entity.
func Import(workspaceDir string, yamlContent []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "flow-import-*.yaml")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(yamlContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command("sap", "import", tmpFile.Name())
	cmd.Dir = workspaceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sap import in %s: %s: %w", workspaceDir, stderr.String(), err)
	}

	// sap import prints the assigned entity ID.
	return strings.TrimSpace(stdout.String()), nil
}

// Remove runs `sap remove <id>` in the given workspace directory.
func Remove(workspaceDir string, id string) error {
	cmd := exec.Command("sap", "remove", id)
	cmd.Dir = workspaceDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sap remove %s in %s: %s: %w", id, workspaceDir, stderr.String(), err)
	}
	return nil
}

// WorkspaceInit runs `sap workspace init <schemaPath>` in the given directory.
func WorkspaceInit(dir string, schemaPath string) error {
	cmd := exec.Command("sap", "workspace", "init", schemaPath)
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sap workspace init in %s: %s: %w", dir, stderr.String(), err)
	}
	return nil
}

// WorkspaceExists checks if a directory looks like a sap workspace (has schema.yaml).
func WorkspaceExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "schema.yaml"))
	return err == nil
}

// HooksRun runs `sap hooks run <event> <entityIDs...>` in the given workspace directory.
// This is fire-and-forget on the sap side, but we still report startup errors.
func HooksRun(workspaceDir string, event string, entityIDs []string) error {
	if len(entityIDs) == 0 {
		return nil
	}
	args := []string{"hooks", "run", event}
	args = append(args, entityIDs...)

	cmd := exec.Command("sap", args...)
	cmd.Dir = workspaceDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		// If no hook is configured, that's not an error for flow.
		if strings.Contains(errMsg, "no hook configured") {
			return nil
		}
		return fmt.Errorf("sap hooks run in %s: %s: %w", workspaceDir, errMsg, err)
	}
	return nil
}
