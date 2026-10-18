# Decision 0004 — Provider-neutral review runtime, disabled by default

- Date: 2026-10-18
- Status: GOV-003B offline implementation decision; activation pending
- Governance source: [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md)

## Decision

Keep the existing `go-githubapp` credential and Check Run boundary in `internal/reviewer`. Add a standard-library-only `internal/reviewworker` protocol and synthetic subprocess transport. The protocol uses bounded, versioned JSON and validates exact SHA, provider/model, complete analysis and added-line findings before the existing reporter sees results. The bridge is private, while production `NewService` retains `UnavailableEngine`. Codex and Claude adapters expose metadata but always return unavailable. This preserves the human-only approval/merge gate.

The process transport is not considered safe for live model execution until a separate OS/container boundary and provider authorization are reviewed. No real CLI invocation, App authentication, GitHub write, paid API or subscription inference occurs. A future Work Item must choose and pin concrete provider command templates only after verification of installed versions, read-only behavior, network/tool policy, terms, identity and cost. Adding a boolean to this package is insufficient activation authority.

## Dependencies and trade-offs

No new direct or transitive dependencies were added. `go.mod` still selects `go-githubapp v0.48.0` for GitHub integration, with its existing pinned transitive modules in `go.mod`/`go.sum`. Go standard library implements protocol validation and process control. Fixed protocol size limits intentionally make large or binary PRs unavailable rather than issuing an incomplete green review. Linux process-group cancellation is tested offline; portability to other OSes and OS resource isolation remain unverified. The memory field is advisory metadata until the worker supervisor enforces it.

Provider documentation reviewed: [OpenAI Cookbook's Codex exec example](https://developers.openai.com/cookbook/examples/codex/build_iterative_repair_loops_with_codex) and [Claude Code headless documentation](https://code.claude.com/docs/en/headless). The local Codex help was read without invoking a model; Claude CLI was absent. Neither documentation nor a model self-report establishes subscription eligibility, network isolation or the right to run unattended reviews. Human approval of entitlement, cost and deployment boundary remains required.
