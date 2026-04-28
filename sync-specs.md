# Flow Remote Sync Spec (Draft)

This document captures the current high-level design for GitHub review-thread sync in `flow`, aligned with the existing `flow`/`sap`/`aether` model and `flow nudge` orchestration.

Scope boundary:

- `flow` provides generic source/transition orchestration and selection/scope semantics.
- `flow` also owns generic sync-safety preflight behavior based on entity metadata (for v1: duplicate detection by `_sync.id` and dedup handling semantics).
- Use-case-specific remote logic (e.g., GitHub fetch/push behavior, payload mapping, repo inference) is implemented in external executables invoked by sources/transitions.

## Goal

Enable a git-like workflow for remote-backed entities:

- Pull remote state into local sap entities.
- Edit the same domain properties locally.
- Push local edits back to the remote system.
- Reject or reconcile when remote changed concurrently.

The design must preserve structured sap entities and avoid an intent side-channel model.

## Core Modeling Decision

Sync is modeled as `flow` sources/transitions, not as separate pull/push top-level commands.

- Pull remains a source (remote -> stage).
- Push is modeled as a transition out of a stage to a remote sink.
- `flow nudge` remains the single orchestration entrypoint.

## Transition Semantics

Introduce a transition mode for side effects without local movement.

Conceptually:

- Existing behavior: move (`from` -> `to`) with optional transform.
- New behavior: sink (`from` -> remote side effect), no required local destination.

For sink transitions:

- Eligible entities are selected from `from` (with existing condition/scope behavior).
- Action script performs remote API writes.
- Entities are not automatically removed/moved by sink execution.
- Success/failure is reported per entity (or per batch) in nudge output.

## Entity Granularity and Shape

For the PR feedback use case:

- One entity = one GitHub review thread.
- Users/agents edit domain properties directly (same object model used for pull and push).
- No separate `planned*`, `toPost`, or diff/intention properties.

Example implication:

- Adding a new comment means adding an item to `comments` in the entity itself.

## Minimal Sync Metadata

A small namespaced sync block on the entity is acceptable.

- `_sync.id`: stable remote identifier (GitHub review thread id).
- `_sync.base`: fingerprint/hash of canonical remote thread state at baseline.
- `_sync.updatedAt`: optional metadata timestamp.

Constraint:

- Keep `_sync` small and technical.
- Keep domain content in domain properties.

## Push Mapping Rules (Domain Edit -> Remote Calls)

Push derives API operations from domain-object deltas relative to baseline.

For review threads:

- `comments` items with `id`: represent already persisted remote comments.
- `comments` items without `id`: represent new local comments to publish.
- `isResolved` change: resolve/unresolve operation (as supported).
- Editing immutable remote comment content (existing `id` comment body/metadata): unsupported; treated as conflict/error.

Push can emit multiple API calls for one entity (e.g., publish comment and resolve).

## Reconcile Model

Use a three-way merge when remote drift is detected.

Definitions per entity:

- `B`: baseline snapshot associated with `_sync.base`.
- `L`: current local entity.
- `R`: current remote entity fetched at push time.

High-level flow:

- If `L == B`: no local edits; fast-forward to `R`.
- If `R == B`: no remote edits; local can push directly.
- Otherwise perform field-aware merge and continue when safe.

### Field-Aware Auto-Merge Policy (v1)

- `comments` list:
  - Treat `id` comments as canonical remote comments.
  - Treat no-`id` comments as local unsent additions.
  - Auto-merge by taking canonical comments from `R` and appending local unsent comments introduced in `L` (vs `B`).
  - Conflict if local edited/deleted canonical `id` comments.
- `isResolved` boolean:
  - Single-side change from `B`: take changed side.
  - Both sides changed to same value: take value.
  - Both sides changed to different values: conflict.
- Other scalars:
  - Single-writer from baseline: take changed side.
  - Divergent double-write: conflict.
- `_sync`:
  - Not user-merged; recomputed by flow.

### Manual Reconcile Required (v1)

When auto-merge cannot resolve safely, v1 uses duplicate local entities with the same `_sync.id` as the conflict representation.

Rules:

- Conflict state is derived from data shape, not an explicit `_sync.conflict` flag.
- A `_sync.id` is considered conflicted when more than one local entity has that `_sync.id`.
- Local entity slugs remain unique and can store multiple conflicting copies.

Conflict behavior:

- Pull: `flow` detects conflicted `_sync.id`s, skips them, and emits a warning.
- Push: `flow` detects conflicted `_sync.id`s, rejects them with an error/warning; other non-conflicted ids may continue.
- Dedup logic is a `flow` concern and must key by `_sync.id` without silently collapsing conflicting local variants into one.

Manual resolve workflow:

- User/agent reconciles conflicting copies locally (merge/edit/delete as needed).
- Resolution is complete when exactly one entity remains for that `_sync.id`.
- After uniqueness is restored, normal pull/push resumes for that `_sync.id`.

Typical causes that enter this flow:

- Local edits touch immutable canonical remote comment fields.
- Same property changed differently on both sides (outside approved auto-merge rules).
- Canonical comment reorder/delete operations are ambiguous.

## Operational Loop (`flow nudge` only)

The intended loop is:

1. `nudge` pull: fetch remote state into local entities, update baseline metadata.
2. Local edits: user/agent edits domain properties directly.
3. `nudge` push: run sink transition to apply remote writes.
4. `nudge` pull: refresh canonical remote state and ids.

Step 4 remains a normal pull/update/nudge, not a separate command. This keeps behavior explicit and consistent with existing flow orchestration.

## Repository/Remote Discovery

Repository/remote inference is use-case logic and therefore belongs to source/sink executables, not `flow` core.

- `flow` passes entities/input context to executables.
- Executables infer repo/remote context (for example from issue directory and git remotes).
- Stable resolved refs (e.g., `repo`, `_sync.id`) are written onto entities by pull-side executables and consumed by push-side executables.

## Non-Goals (Current Draft)

- No standalone `flow pull` / `flow push` commands.
- No large sync journal/history in entity payloads.
- No requirement to model sync state in a separate sap space for v1.
- No use-case-specific GitHub behavior embedded in `flow` core.

## Todo (still underspecified)
- "Repository/Remote Discovery" still underspecified
