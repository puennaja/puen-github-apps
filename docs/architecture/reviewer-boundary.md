# GOV-003A Reviewer boundary

Status: offline MVP; live webhook, App actor, permission enforcement and AI inference **NOT RUN**. Governance remains [puen-engineer ADR-0002](https://github.com/puennaja/puen-engineer/blob/master/decisions/0002-provider-neutral-pr-governance.md). The existing Implementer source is deferred, not removed; the current implementer identity is independent from the Reviewer App and Human Gate Owner.

```text
GitHub pull_request webhook
  -> size/method/header guard -> go-githubapp HMAC verification
  -> installation/repository/base/action/draft policy -> bounded queue
  -> exact-head PR/files read + protected-base requirements read
  -> Engine interface (UnavailableEngine in GOV-003A)
  -> exact-head recheck -> SHA-bound Check Run
  -> human approval and merge outside this App
```

## Trust and state

The webhook process contains the App private key, webhook secret and short-lived installation token. The future AI engine process must be separately isolated and receive only bounded inert evidence over an authenticated private channel. There is no untrusted subprocess in GOV-003A, and no live host is supplied. Source files, patches, PR body and changed documents are **data**, never privileged instructions. Requirements are read at the PR's protected base SHA; changed requirements remain visible in the diff but cannot rewrite the review policy. `AGENTS.md` at base is mandatory. Optional existing architecture, spec/ADR and test-evidence Markdown is included when available.

Accepted webhook actions: `opened`, `synchronize`, `ready_for_review`, `reopened`. Draft is `DRAFT_SKIPPED`. Duplicate delivery IDs are ignored within a bounded in-memory window; full queues return 503 without marking a delivery seen. Delivery IDs and latest head per PR are in-memory only, so restart-safe deduplication and durable work recovery are activation blockers. A queued older SHA is suppressed when a newer delivery arrives. Before reporting, the orchestrator re-reads PR head/state and checks the local latest SHA. A final cross-call race is possible; Check Runs are attached to the reviewed SHA, so a run on an old commit cannot be mistaken for current-head evidence.

The maximum webhook is 2 MiB; queue capacity is configured up to 1024; recent-delivery window is 2048. Review input is limited to 100 changed files, 64 KiB per patch, 512 KiB total patch, 32 KiB PR body, 12 requirement files, 32 KiB each and 256 KiB total requirements. If the first page has exactly 100 files, the adapter requests page 2 with the same page size and rejects any additional file or paging error. Missing/truncated patches or required evidence fail closed. Results are limited to 20 findings, with strict severity and added-line checks before annotations. These limits intentionally mark some large/binary PRs `REVIEW_UNAVAILABLE`.

Each job retries at most three times for pre-report failures. Complete analysis with BLOCKER/MAJOR yields `BLOCKED`; complete analysis without them yields advisory `READY_FOR_HUMAN_REVIEW`; unavailable/incomplete/failure yields an `action_required` `REVIEW_UNAVAILABLE` Check Run where reporting remains possible. GitHub counts `neutral` as passing for required checks, so it is deliberately not used ([GitHub rule](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks)). A stale head is suppressed and a changed base is `POLICY_DENIED`, with no publication. If the GitHub write itself fails, in-memory status is `REPORT_FAILED`; the remote effect is unknown and the write is not automatically retried. Operational reconciliation is required. No status grants approval or merge authority. A stale or Draft head is not marked ready. API transport errors are sanitized and credentials are never passed to the engine or written to findings.

## Private GitHub operations and activation

The `go-githubapp` dispatcher verifies webhook signatures. Its unscoped `NewInstallationClient` is not used. A private App client verifies App/installation identity and exact installation permissions, requests a token for exactly one numeric repository ID and the exact read/check-write permission map, then validates the response's repository list, permissions and expiry. Only then is a private framework token client created. The adapter allows only PR GET, PR file-list GET, base-commit contents GET and Check Run POST. It has no approval, merge, contents write or generic agent-facing GitHub client. Network is disabled by default, fixed to GitHub HTTPS, with redirects/proxies denied and bounded responses.

Before activation, the Human Gate Owner must record actual App ID/name, installation ID, selected repositories and IDs, exact permissions, authenticated bot actor, webhook/private-key custody and rotation, token lifetime/revocation, isolated engine bridge, non-secret audit retention, live positive/negative tests and rollback. A production supervisor must provide durable queue/dedup/status storage, monitoring and a safe stop/replay procedure. No registration, credentials, live token issuance, live write, model invocation or ruleset change is part of this PR. A separately approved activation can stop the listener, drain/disable queue, revoke/suspend installation and reconcile Check Runs; this rollback is **NOT RUN**.
