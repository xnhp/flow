

# Overview

`flow` is a pipeline tool that moves entities between `sap` workspaces according to declared transitions. It sits on top of `sap` and composes `sap` CLI commands with external scripts.

`sap` stays pure: workspaces, entities, schemas, query, import, TUI. No knowledge of pipelines.

`flow` reads a pipeline config declaring stages (workspace paths) and transitions between them. Each transition has a condition (query expression) and an action (executable). `advance` evaluates: for each transition, run `sap query` on the source workspace, find eligible entities, run the executable, `sap import` the result into the destination workspace (removing from the source).

## Relation to `sap`

`sap` is a pure entity tool: workspaces, schemas, query, import, TUI. It has no knowledge of pipelines, transitions, or orchestration.

`flow` composes `sap` CLI commands (`sap query`, `sap import`, `sap remove`) with external executables. Schema validation happens at stage boundaries via `sap import` -- that's a property of the workspace, not the pipeline.

# Action scripts

Action scripts do the actual work: fetching from APIs, transforming data, invoking agents, posting to remotes. They don't need to know about `sap` or `flow` -- they're just programs that transform data.

## Requirements

- Action scripts must be fully agnostic of `sap` and `flow`. They must not directly interact with either tool.
- They receive entity content on stdin and produce transformed content on stdout.
- They must be either sufficiently defensive or atomic enough that failure means "nothing happened" -- no partial side effects left behind. E.g. a script that posts a GitHub comment and then resolves the thread must handle the case where posting succeeds but resolving fails, such that re-running is safe.
- They must be concurrency-friendly: multiple instances of the same script may run in parallel for different entities within the same transition. Scripts must not assume exclusive access to shared resources (e.g. a git branch) without handling contention.
- For source (fetch) scripts: they return all matching items from the external system. Dedup is `flow`'s responsibility, not the script's.


# Pipeline config

```yaml
stages:
  - workspace: ./fetched
  - workspace: ./filtered
  - workspace: ./triaged
  - workspace: ./implemented
  - workspace: ./posted

transitions:
  - from: fetched
    to: filtered
    condition: "not (isResolved == true or isOutdated == true)"
    # no executable needed -- pure filter

  - from: filtered
    to: triaged
    run: triage-pr-item/run

  - from: triaged
    to: implemented
    condition: "ready == true"
    run: implement-feedback

  - from: implemented
    to: posted
    condition: "committed == true"
    run: post-and-resolve
```

Stages are sap workspace paths. Transitions describe movement between stages: a source workspace, a destination workspace, optionally a condition (sap query expression), and optionally an executable.


# Transition scope

## Motivation

Not all transitions are per-entity. Some actions naturally operate on batches -- e.g. an AI agent addressing multiple PR feedback items in one session benefits from shared context (seeing all items together, making coordinated changes, operating within a single context window).

## Options

- **entity** (default): action receives one entity on stdin, produces one entity on stdout. One invocation per eligible entity.
- **batch**: action receives all eligible entities at once, produces results for all of them. One invocation total.

## ID passing contract

There are no flow-specific IDs. Entity identity within a workspace is managed by `sap` (filenames). Cross-stage tracing and dedup use remote references -- properties on the entity that identify the corresponding item in the external system (e.g. `id`, `permalink`). See "Source stages and dedup".

For batch-scope transitions, the action script receives a list of entities and produces a list of results. Since there are no flow-injected IDs, the matching between input and output is based on the domain data itself (e.g. the remote reference property). If the script produces new entities that don't correspond to any input, they are simply imported as new items.

## Config sketch

```yaml
- from: triaged
  to: implemented
  condition: "ready == true"
  run: implement-feedback
  scope: batch          # all eligible entities at once

- from: triaged
  to: implemented
  condition: "ready == true"
  run: implement-feedback
  scope: entity         # one at a time (default)
```


# Entity identity across stages

## Motivation

When an entity moves from one stage to another, it should be traceable as "the same item." But the schema changes between stages (a triaged item has different fields than an implemented item). So the entity is "the same thing" but with an evolving shape.

## Decisions

- There are no flow-specific IDs. `flow` does not assign or manage entity identity.
- Within a workspace, `sap` manages identity (filenames).
- Across stages, entities are traced by their remote reference -- a domain property like `id` or `permalink` that identifies the corresponding item in the external system.
- For source dedup, `flow` uses the remote reference to check whether an item already exists in any stage. See "Source stages and dedup".
- Action scripts just transform entity content. They don't need to handle identity at all.


# Source stages and dedup

## Motivation

Source stages fetch from external systems (e.g. PR comments via REST API). Unlike internal transitions, the fetch script returns *all* matching items every time. Without dedup, each `advance` would re-feed the same unresolved comment into the pipeline until it eventually gets resolved on the remote.

## Decision

Dedup across all stages in the pipeline. Before importing a fetched entity, `flow` checks whether an entity with the same remote reference exists in *any* stage of the pipeline. If it does, skip it.

The fetch script includes a property (e.g. `id`, `permalink`) that serves as a stable remote key. The pipeline config declares which property to use for dedup:

```yaml
source:
  run: gh--current-pr-feedback
  dedup_key: id    # property name to check across stages
```


# `flow advance` behaviour

## DAG-order evaluation

The pipeline is a DAG, not necessarily a linear chain. Multiple transitions can feed into the same stage, and one stage can fan out to multiple destinations.

`flow advance` evaluates transitions in topological order (DAG order) in a single pass. This means an entity moved from stage A to stage B in an earlier transition can be picked up by the B→C transition in the same `advance` call -- cascading through multiple stages in one invocation.

## Source fetching

Source transitions (fetching from external systems) are evaluated as part of `advance`, as the first step before internal transitions. This means every `advance` call checks for new items from external sources, then processes the full pipeline.

`flow advance --no-sources` skips source transitions, only processing items already in the pipeline. Useful when you want to avoid API calls or just push existing items forward.

## Concurrency within a transition

When a transition has multiple eligible entities, they are processed concurrently. Action scripts must be concurrency-friendly (see "Action scripts > Requirements").

Batch-scope transitions are a single invocation by definition, so concurrency doesn't apply there.

## Partial progress on failure

If processing fails for some entities within a transition, the successfully processed entities are still moved to the destination workspace. Failed entities stay in their current stage. `advance` logs the errors and continues with the next transition in DAG order.

Entities that were successfully moved to a destination stage by an earlier transition are eligible for pickup by later transitions in the same `advance` call.


# Reactive transitions / triggering

## Motivation

When a user (or agent) unblocks an item -- e.g. sets `ready` to `true` -- downstream tasks such as committing, posting a comment, etc. should run automatically. The pipeline is not just a batch processor; it reacts to state changes.

## Decision

`flow advance` evaluates all transitions. For each transition, it queries the source workspace for eligible entities and moves them through the action to the destination workspace.

The trigger for `flow advance` is a separate concern from `flow` itself:
- TUI calls `advance` after save -- reactive during interactive use
- Agent calls `advance` after modifying entities -- reactive during automated use
- A filesystem watcher calls `advance` on changes -- fully automatic
- User calls `advance` manually -- full control

An entity's current stage is defined by which workspace it's in. Once it moves out of a stage, `sap query` no longer finds it there. This makes `advance` naturally idempotent -- running it twice produces the same result.


# Failure handling

## Decisions

- Action scripts must be either sufficiently defensive or atomic enough that failure = "nothing happened" (no partial side effects left behind).
- If an action fails, the entity stays in its current stage, the error is logged by `advance`, and processing continues with other eligible entities.
- No retry logic, no explicit `failed` state -- keep it simple for now.
- An entity is not moved to the destination workspace if its associated action failed.


# Remote state divergence

## Motivation

What if the remote state changes while an entity is in-flight in the pipeline? E.g. a PR author resolves a thread themselves while we are still processing it locally.

## Decisions

- Don't try to update in-flight entities with remote state changes.
- Handle divergence at the point of side effect: if posting fails because the thread is already resolved, log it, discard the entity (or move it to a terminal stage), notify the user.
- Accept that some work may be wasted -- that's fine.
- The key requirement is that divergence is communicated to the user, not silently swallowed.


# Manual injection

Entities can be added to any stage manually via `sap import`. As long as the entity conforms to the workspace schema, `flow` treats it like any other entity on the next `advance`.

If a downstream stage assumes entities correspond to a remote item (e.g. a PR thread), but manually added entities don't, a filter stage before that action handles this -- it's not `flow`'s problem to solve.


# Cross-pipeline aggregation

The "overview of all threads" / "what needs my attention" is not a `flow` concern. It operates at a higher level, collecting from several pipeline instances. Implementable as a script that runs `sap query` across relevant workspaces from multiple pipelines.
