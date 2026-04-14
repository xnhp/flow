// Package cli implements the flow command-line interface.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"flow/internal/config"
	"flow/internal/pipeline"
	"flow/internal/sap"
)

func Run(args []string) error {
	if len(args) == 0 {
		printHelp(os.Stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printHelp(os.Stdout)
		return nil
	case "nudge":
		return runNudge(args[1:])
	case "status":
		return runStatus(args[1:])
	default:
		return fmt.Errorf("unknown command %q, run 'flow help' for usage", args[0])
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "flow - move entities between sap workspaces via transitions")
	fmt.Fprintln(w, "Use flow for orchestration; keep entity editing/validation in sap.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  flow nudge [flags]      Evaluate all transitions and move eligible entities")
	fmt.Fprintln(w, "  flow status             Show entity counts per stage")
	fmt.Fprintln(w, "  flow help               Show this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Nudge flags:")
	fmt.Fprintln(w, "  --no-sources            Skip source fetching (only process existing entities)")
	fmt.Fprintln(w, "  --verbose               Show detailed progress")
	fmt.Fprintln(w, "  --config <path>         Path to flow.yaml (default: ./flow.yaml)")
}

func runNudge(args []string) error {
	var opts pipeline.AdvanceOpts
	configPath := "flow.yaml"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-sources":
			opts.NoSources = true
		case "--verbose", "-v":
			opts.Verbose = true
		case "--config", "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("--config requires a path argument")
			}
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	p, baseDir, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	return pipeline.Advance(p, baseDir, opts)
}

func runStatus(args []string) error {
	configPath := "flow.yaml"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("--config requires a path argument")
			}
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	p, baseDir, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	for _, stage := range p.Stages {
		name := config.StageName(stage)
		dir := filepath.Join(baseDir, stage.Workspace)

		var count int
		if sap.WorkspaceExists(dir) {
			ids, err := sap.AllEntityIDs(dir)
			if err == nil {
				count = len(ids)
			}
		}

		fmt.Printf("%-20s %d entities\n", name, count)
	}

	return nil
}
