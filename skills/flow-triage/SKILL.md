---
name: flow-triage
description: "Use when working with PR feedback items using flow & sap"
---

related skills: flow, sap

For the items in `filtered`, triage them according to the instructions below and
enter contents to `todo`, `response_comment`, `edit_suggestions` etc. as applicable.

- Make sure the local checkouts are up to date, if there are newer commits on the remote tracking branch then pull them in (rebase, not merge).
- qualify/triage the item by reviewing related code etc
- make concrete suggestions for to-dos
- if trivial/auto-approved, directly implement and commit, set `done` to true

## Trivial definition for auto-approve
- Determine whether the item is trivial by reviewing the related code etc.
Treat as trivial only when the change is very low-risk and behavior-neutral, e.g.:
- formatting
- style
- naming hygiene
- mechanical convention updates
- already implemented

## response comment guidelines
- Stay impartial of the quality of the existing code or the change. Never commend ("Good point") or discourage ("I am sorry for missing this").
  Just use neutral, professional, technical tone. Be direct.
- Describe the change as-is, not as-will-be.
- response comments do not have to reference a commit hash (we will do that synthetically when posting the comment)

### Positive example
```
Addressed: Changed flombuuble to use clearer type annotations
```
### Negative example (don't do this)
```
Good point, thanks for catching this. I'll do this and that and not that or this.
```
    
