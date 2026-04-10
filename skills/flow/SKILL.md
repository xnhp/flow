---
name: flow
description: "Orchestrate movement of entities between sap workspaces."
---

# flow

- `flow` is the orchestration layer on top of `sap`.
- It moves entities between stage workspaces based on declarative transitions in `flow.yaml`.
- Source stages fetch external items; transition stages filter/transform/move items.
- `flow` does not edit entity content itself; use `sap` (or scripts/agents) for that.
- `flow` triggers `sap` post-import hooks after successful imports.

## Start Here
- From the pipeline directory, run `flow status` to see stage counts.
- Run `flow advance` to fetch/process/move entities.
- Use `flow advance --no-sources` when you only want to process already-fetched items.
- Use `--help` for exact command/flag details.
