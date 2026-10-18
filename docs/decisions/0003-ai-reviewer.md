# Decision 0003 — Provider-neutral AI Reviewer offline MVP

- Date: 2026-10-18
- Status: GOV-003A implementation decision; activation and AI provider decision pending
- Governance source: [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md)

## Decision

Use Go and the existing pinned `github.com/palantir/go-githubapp v0.48.0` as the only directly selected GitHub integration framework. It supplies App JWT/client creation, a static-token client and a webhook dispatcher that verifies signatures. The in-house Go standard-library code owns exact repository/installation policy, one-repository token-scope request/response validation, queue/deduplication, exact-SHA orchestration, context limits, engine boundary and Check Run formatting. `NewInstallationClient` is not used because it cannot request a narrowly scoped token. No additional GitHub SDK is directly imported or required by reviewer code.

Use Check Runs instead of PR comments. That allows `Pull requests: read` plus `Checks: write` without PR write, and ties each outcome to a reviewed commit. A completed review with BLOCKER/MAJOR is a failing check; a complete review without those findings is a successful **advisory** check. Any unavailable/incomplete analysis uses `action_required`, never `neutral` or `success`: GitHub treats `neutral` as passing for required checks. GitHub human Approval and Merge remain solely with the Human Gate Owner.

No real engine is selected. `Engine` accepts bounded inert evidence and returns structured findings; the exported production constructor always selects `UnavailableEngine`, while the mock-engine constructor is private to package-local tests. An embedding cannot supply a readiness-capable engine without a separately reviewed code change. Subscription-backed or local CLI integration requires a separate, approved assessment of terms, unattended access, isolation, identity, cost and safe invocation. It must not share the App's credentials, process, OS context or unrestricted filesystem. No paid AI API is called.

## Dependencies and limits

`go.mod` directly selects only `go-githubapp v0.48.0`. Its pinned transitive modules are `ghinstallation/v2`, `go-github/v88` and `/v92`, `githubv4`, `oauth2`, JWT, `zerolog`, metrics, HTTP cache/LRU and support modules; exact versions and checksums remain in `go.mod` and `go.sum`. The framework's dispatcher verifies signatures but does not implement this application's allowlists, durable queue or AI isolation. Generic framework REST clients can call broader endpoints, so they remain private behind four fixed operations. Check Run creation requires a live App installation and cannot be validated against GitHub offline.

The in-memory delivery window, queue and statuses are not crash durable. An API read/review/report race remains; checks are SHA-bound and current-head checks are repeated. Large/binary diffs are unavailable rather than silently reviewed. No actual App actor, entitlement, token scoping, webhook delivery, Check Run permission, reviewer independence or human merge enforcement has been proven live. Operator approval and recorded evidence are required before deployment. GOV-001A Implementer source remains deferred in the current identity workflow, not deleted or modified by this decision.
