# Architecture overview — proposed

```text
Codex / Claude Code / other authorized implementation tool
    -> Implementation Agent gateway / identity (proposed)
    -> feature branch + Draft PR
    -> deterministic PR preflight + independent AI Reviewer (future)
    -> human review/approval -> human-only merge
```

## Separate identities and actions
- Role != provider != Git commit author != GitHub authenticated actor.
- Implementation Agent writes scoped branches and opens PRs.
- AI Reviewer comments independently and reports reviewed HEAD SHA; cannot count as human approval.
- Human Gate Owner is final authority for merge and high-risk permissions.

## State contract
Draft -> preflight -> independent review (finding / no blocker / incomplete) -> human decision -> merge.

Preflight does not equal code review; AI review does not equal human approval; a changed head invalidates stale review. Failed/unavailable reviewer must not become a successful status.

## Source of truth
See [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md). No bot or runtime automation exists as part of this bootstrap.
