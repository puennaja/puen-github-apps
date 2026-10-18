# Architecture overview

GOV-001A implements an offline-tested Implementation Agent boundary described in
[implementer-boundary.md](implementer-boundary.md), but this App integration is
**deferred** in the current workflow. GOV-003A implements the offline Reviewer
boundary in [reviewer-boundary.md](reviewer-boundary.md). Neither App is installed
or enabled; live enforcement is **NOT RUN**.

```text
Codex / Claude Code / other authorized implementation tool
    -> independent GitHub identity -> PR
    -> signed webhook -> AI Reviewer boundary (offline MVP; engine unavailable)
    -> exact-SHA advisory Check Run
    -> human review/approval -> human-only merge
```

## Separate identities and actions
- Role != provider != Git commit author != GitHub authenticated actor.
- Implementation Agent writes scoped branches and opens PRs using an independent
  GitHub identity in the current workflow; its App integration is deferred.
- AI Reviewer comments independently and reports reviewed HEAD SHA; cannot count as human approval.
- Human Gate Owner is final authority for merge and high-risk permissions.

## State contract
Draft -> preflight -> independent review (finding / no blocker / incomplete) -> human decision -> merge.

Preflight does not equal code review; AI review does not equal human approval; a changed head invalidates stale review. Failed/unavailable reviewer must not become a successful status.

## Source of truth
See [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md). No live bot or runtime automation is enabled by GOV-001A or GOV-003A.
